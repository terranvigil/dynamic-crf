package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// Cambi measures banding in an encode with Netflix's CAMBI. CAMBI is a
// no-reference metric, so only the distorted file is read; the vmaf CLI still
// wants two inputs, so it's handed the distorted on both.
type Cambi struct {
	logger        *slog.Logger
	distortedPath string
	opts          VMAFOptions
}

// CambiResult is CAMBI pooled over frames. Higher means more banding; under
// 5 is generally not visible.
type CambiResult struct {
	Max  float64
	Mean float64
}

func NewCambi(logger *slog.Logger, distorted string, opts VMAFOptions) *Cambi {
	return &Cambi{
		logger:        logger,
		distortedPath: distorted,
		opts:          opts,
	}
}

func (c *Cambi) Run(ctx context.Context) (*CambiResult, error) {
	dist, err := ProbeVideo(ctx, c.logger, c.distortedPath)
	if err != nil {
		return nil, fmt.Errorf("probe distorted: %w", err)
	}

	if max(dist.Width, dist.Height) < cambiMinLongSide {
		return nil, fmt.Errorf("CAMBI can't score a %dx%d picture (it needs a long side of at least %d)",
			dist.Width, dist.Height, cambiMinLongSide)
	}
	if dist.Width+dist.Height > cambiMaxWidthPlusHeight {
		return nil, fmt.Errorf("CAMBI can't score a %dx%d picture (libvmaf's limit is width plus height %d, just past UHD 4K); scale it down first",
			dist.Width, dist.Height, cambiMaxWidthPlusHeight)
	}
	leg := pipeLeg{input: c.distortedPath, pixFmt: cambiPixFmt(dist.BitDepth)}
	args := []string{"--threads", fmt.Sprint(c.opts.threads()), "--no_prediction", "--feature", "cambi"}

	c.logger.Info("running cambi", "distorted", c.distortedPath, "pixFmt", leg.pixFmt)
	out, err := runVMAFPipes(ctx, c.logger, c.opts.bin(), leg, leg, args)
	if err != nil {
		return nil, fmt.Errorf("cambi of %s: %w", c.distortedPath, err)
	}
	pooled, ok := out.PooledMetrics["cambi"]
	if !ok {
		return nil, errors.New("vmaf output has no cambi pooled metric")
	}
	return &CambiResult{Max: pooled.Max, Mean: pooled.Mean}, nil
}

// standalone CAMBI's window outgrows libvmaf 3.2.1's lookup table just past
// UHD 4K: width plus height 6,092 works (3932x2160), 6,094 doesn't
const cambiMaxWidthPlusHeight = 6092

// below this on its longer side libvmaf can't read the picture for CAMBI
const cambiMinLongSide = 216

// cambiPixFmt keeps the encode's own depth: CAMBI's banding thresholds depend on it.
func cambiPixFmt(bitDepth int) string {
	if bitDepth > 8 { //nolint:mnd
		return "yuv420p10le"
	}
	return "yuv420p"
}
