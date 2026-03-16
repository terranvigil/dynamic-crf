package actions

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/inspect"
	"github.com/terranvigil/dynamic-crf/model"
)

const (
	maxSearchIterations       = 20
	defaultSceneSampleMinDurMs = 60 * 1000
)

// CrfSearch will perform a hybrid bisection/interpolation search over CRF values
// to find the closest match to the target VMAF
type CrfSearch struct {
	logger          *slog.Logger
	sourcePath      string
	targetVMAF      float64
	crfInitial      int
	crfMin          int
	crfMax          int
	tolerance       float64
	transcodeConfig commands.TranscodeConfig
	// scoreFn allows overriding the scoring function for testing
	scoreFn func(ctx context.Context, samplePath string, crf int) (float64, error)
	// samplePath overrides sample creation for testing
	samplePath string
	// sceneSampleMinDurMs is the minimum source duration (ms) before scene sampling kicks in.
	// Zero means use the default (60s).
	sceneSampleMinDurMs float64
}

func NewCrfSearch(logger *slog.Logger, source string, targetVMAF float64, crfInitial int, crfMin int, crfMax int, tolerance float64, transcodeConfig commands.TranscodeConfig) *CrfSearch {
	return &CrfSearch{
		logger:          logger,
		sourcePath:      source,
		targetVMAF:      targetVMAF,
		crfInitial:      crfInitial,
		crfMin:          crfMin,
		crfMax:          crfMax,
		tolerance:       tolerance,
		transcodeConfig: transcodeConfig,
	}
}

func (c *CrfSearch) Run(ctx context.Context) (selected int, vmaf float64, err error) {
	c.logger.Info("running dynamic-crf search")

	if err := c.validate(); err != nil {
		return 0, 0, err
	}

	score := c.getScoreFn()

	var sampleName string
	if c.samplePath != "" {
		sampleName = c.samplePath
	} else {
		var source *os.File
		if source, err = os.Open(c.sourcePath); err != nil {
			return 0, 0, fmt.Errorf("failed to open source: %s, err: %w", c.sourcePath, err)
		}
		defer source.Close() //nolint:errcheck

		var metadata *model.MediaInfo
		if metadata, err = commands.NewMediaInfo(c.logger, c.sourcePath).Run(ctx); err != nil {
			return 0, 0, fmt.Errorf("failed to get mediainfo of source, err: %w", err)
		}
		minDurMs := c.sceneSampleMinDurMs
		if minDurMs == 0 {
			minDurMs = defaultSceneSampleMinDurMs
		}
		// mediainfo reports duration in ms for most containers, but in seconds
		// for raw formats like y4m. Normalize to ms.
		durationMs := metadata.GetContainer().Duration
		if durationMs < 1000 {
			durationMs *= 1000
		}
		if durationMs < minDurMs {
			c.logger.Info("source is less than 60 seconds, skipping creation of shots sample")
			sampleName = source.Name()
		} else {
			sampleEncode, err := c.createSceneSample(ctx)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to create scene sample, err: %w", err)
			}
			defer os.Remove(sampleEncode.Name()) //nolint:errcheck
			sampleName = sampleEncode.Name()
		}
	}

	// get vmaf of initial CRF
	var initialScore float64
	if initialScore, err = score(ctx, sampleName, c.crfInitial); err != nil {
		return selected, vmaf, err
	}
	if checkScore(initialScore, c.targetVMAF, c.tolerance) {
		c.logger.Info("found vmaf at initial crf", "vmaf", initialScore, "crf", c.crfInitial)
		return c.crfInitial, initialScore, nil
	}
	if initialScore > c.targetVMAF {
		c.crfMax = c.crfInitial
	} else {
		c.crfMin = c.crfInitial
	}

	low := 0
	high := c.crfMin - c.crfMax
	if high <= 0 {
		return c.crfInitial, initialScore, nil
	}

	scores := make([]float64, high+1)
	if c.crfMax == c.crfInitial {
		scores[high] = initialScore
	} else if c.crfMin == c.crfInitial {
		scores[low] = initialScore
	}

	if scores[low] == 0 {
		if scores[low], err = score(ctx, sampleName, c.crfMin); err != nil {
			return selected, vmaf, err
		}
		if checkScore(scores[low], c.targetVMAF, c.tolerance) {
			c.logger.Info("found vmaf at min crf", "vmaf", scores[low], "crf", c.crfMin)
			return c.crfMin, scores[low], nil
		}
	}

	if scores[high] == 0 {
		if scores[high], err = score(ctx, sampleName, c.crfMax); err != nil {
			return selected, vmaf, err
		}
		if checkScore(scores[high], c.targetVMAF, c.tolerance) {
			c.logger.Info("found vmaf at max crf", "vmaf", scores[high], "crf", c.crfMax)
			return c.crfMax, scores[high], nil
		}
	}

	c.logger.Info("initial VMAF range", "low", scores[low], "high", scores[high])

	if scores[high] < c.targetVMAF {
		c.logger.Info("target vmaf unreachable at max crf, selecting max", "target", c.targetVMAF, "vmaf", scores[high], "crf", c.crfMax)
		return c.crfMax, scores[high], nil
	}

	// hybrid bisection/interpolation search
	// bisection guarantees convergence (halves range each step)
	// interpolation biases the midpoint toward the expected position
	// blend: 70% bisection, 30% interpolation to handle VMAF's non-linear CRF relationship
	var lastPos, curPos, curCRF int
	for i := range maxSearchIterations {
		if low >= high {
			break
		}
		if c.targetVMAF < scores[low] || c.targetVMAF > scores[high] {
			break
		}

		mid := (low + high) / 2

		// interpolation with division-by-zero guard
		interp := mid
		denom := scores[high] - scores[low]
		if denom > 0 {
			interp = low + int(math.Round(float64(high-low)*(c.targetVMAF-scores[low])/denom))
		}

		// blend bisection and interpolation
		curPos = int(math.Round(0.7*float64(mid) + 0.3*float64(interp)))

		// clamp to valid search range (must make progress)
		if curPos <= low {
			curPos = low + 1
		}
		if curPos >= high {
			curPos = high - 1
		}

		if curPos == lastPos {
			break
		}

		curCRF = c.crfMin - curPos
		c.logger.Info("searching", "iteration", i+1, "position", curPos, "crf", curCRF)

		if scores[curPos], err = score(ctx, sampleName, curCRF); err != nil {
			return selected, vmaf, err
		}

		if checkScore(scores[curPos], c.targetVMAF, c.tolerance) {
			break
		}

		if scores[curPos] > c.targetVMAF {
			high = curPos
		} else {
			low = curPos
		}
		lastPos = curPos
	}

	selected = curCRF
	vmaf = scores[curPos]
	c.logger.Info("found vmaf", "vmaf", vmaf, "crf", selected)

	return selected, vmaf, nil
}

func (c *CrfSearch) validate() error {
	if c.crfMin <= c.crfMax {
		return fmt.Errorf("min crf (%d) must be greater than max crf (%d)", c.crfMin, c.crfMax)
	}
	if c.crfInitial < c.crfMax || c.crfInitial > c.crfMin {
		return fmt.Errorf("initial crf (%d) must be between max (%d) and min (%d)", c.crfInitial, c.crfMax, c.crfMin)
	}
	if c.targetVMAF <= 0 || c.targetVMAF > 100 {
		return fmt.Errorf("target vmaf (%.2f) must be between 0 and 100", c.targetVMAF)
	}
	if c.tolerance <= 0 {
		return fmt.Errorf("tolerance (%.2f) must be positive", c.tolerance)
	}
	return nil
}

func (c *CrfSearch) getScoreFn() func(ctx context.Context, samplePath string, crf int) (float64, error) {
	if c.scoreFn != nil {
		return c.scoreFn
	}
	return c.runScore
}

func (c *CrfSearch) runScore(ctx context.Context, samplePath string, crf int) (float64, error) {
	currentConfig := c.transcodeConfig
	currentConfig.VideoCRF = crf
	score, averageBitrateKBPS, maxBitrateKBPS, streamSize, err := NewVMAFEncodeScore(c.logger, currentConfig, samplePath).Run(ctx)
	if err != nil {
		return 0, err
	}

	streamSizeKB := message.NewPrinter(language.English).Sprintf("%d", streamSize)
	c.logger.Info("score result",
		"score", fmt.Sprintf("%.2f", score),
		"avgBitrate", fmt.Sprintf("%dKbps", averageBitrateKBPS),
		"maxBitrate", fmt.Sprintf("%dKbps", maxBitrateKBPS),
		"bufferSize", fmt.Sprintf("%dKbs", currentConfig.VideoBufferSizeKbps),
		"fileSize", streamSizeKB+"KB",
	)

	return score, nil
}

func checkScore(vmaf float64, targetVmaf float64, tolerance float64) bool {
	return math.Abs(vmaf-targetVmaf) < tolerance
}

func (c *CrfSearch) createSceneSample(ctx context.Context) (*os.File, error) {
	sampleEncode, err := os.CreateTemp("", "sample_*.mp4")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file for encoding sample: %w", err)
	}

	cleanup := func() { os.Remove(sampleEncode.Name()) } //nolint:errcheck

	scenes, fps, err := inspect.DetectScenes(c.logger, ctx, c.sourcePath)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to detect scenes in source, err: %w", err)
	}
	if err = commands.NewFfmpegSampler(c.logger, c.sourcePath, sampleEncode.Name(), fps, scenes).Run(ctx); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to sample source, err: %w", err)
	}

	return sampleEncode, nil
}
