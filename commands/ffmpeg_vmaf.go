package commands

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/model"
)

const vmafREString = "VMAF score: (([0-9]*[.])?[0-9]+)"

var vmafRE = regexp.MustCompile(vmafREString)

type FfmpegVMAF struct {
	logger           zerolog.Logger
	referencePath string
	distortedPath string
	// 1-10, 1 is slowest, 10 is fastest, tradeoff between speed and accuracy
	speed		   int
}

func NewFfmpegVMAF(logger zerolog.Logger, distorted string, reference string, speed int) *FfmpegVMAF {
	return &FfmpegVMAF{
		logger:           logger,
		distortedPath: distorted,
		referencePath: reference,
		speed: speed,
	}
}

// TODO finished #1, need to implement 2-8

// Requirements:
// 1. Distorted and reference must have the same resolution, scale distorted if not
// 2. Distorted and reference must have the same framerate, resample distorted if not
// 3. Distorted and reference must have the same duration, trim the longer of the two if not
// 4. Distorted and reference must have the same color space, convert distorted if not
// 5. Distorted and reference must have the same color range, convert distorted if not
// 6. Distorted and reference must both be progressive or both be interlaced, convert either if not
// 7. Distorted and reference must have the same pixel aspect ratio, convert distorted if not
// 8. Scaling algorithm used to scale reference should be the same used to scale the distorted
//      when it was originally encoded
func (v *FfmpegVMAF) Run(ctx context.Context) (score float64, err error) {
	var stderr bytes.Buffer

	// get reference an distorted metadata
	var refMeta, distMeta *model.MediaInfo
	if refMeta, err = NewMediaInfo(v.logger, v.referencePath).Run(ctx); err != nil {
		err = fmt.Errorf("failed to get mediainfo of reference, err: %w", err)
		return
	}
	if distMeta, err = NewMediaInfo(v.logger, v.distortedPath).Run(ctx); err != nil {
		err = fmt.Errorf("failed to get mediainfo of distorted, err: %w", err)
		return
	}

	var refW, refH, distW, distH int
	if len(refMeta.GetVideoTracks()) == 0 {
		err = fmt.Errorf("reference has no video tracks")
		return
	}
	if len(distMeta.GetVideoTracks()) == 0 {
		err = fmt.Errorf("distorted has no video tracks")
		return
	}
	refW = refMeta.GetVideoTracks()[0].Width
	refH = refMeta.GetVideoTracks()[0].Height
	distW = distMeta.GetVideoTracks()[0].Width
	distH = distMeta.GetVideoTracks()[0].Height

	threads := 4
	if v.speed > 1 {
		cores := runtime.NumCPU()
		// TODO could increase relative to speed as well
		threads = cores - 1
	}

	args := []string{
		"-hide_banner",
		"-i", v.distortedPath,
		"-i", v.referencePath,
		"-an",
	}

	vmaf := fmt.Sprintf("libvmaf='n_threads=%d:n_subsample=%d'", threads, v.speed)

	if refW != distW || refH != distH {
		// Note there may be cases where it is better to scale the reference to the distorted
		// resolution, e.g. the referende is 4K and the distorted is 1080p
		args = append(args, "-filter_complex", fmt.Sprintf("[0:v]zscale=%d:%d:flags=spline36[distorted],[distorted][1:v]%s", refW, refH, vmaf))
	} else {
		args = append(args, "-lavfi", vmaf)
	}
	args = append(args, "-f", "null", "-")

	v.logger.Info().Msgf("ffmpeg args: %v", args)
	v.logger.Info().Msg("running ffmpeg vmaf")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		err = fmt.Errorf("ffmpeg vmaf of distorted: %s, reference: %s failed, err: %w, message: %s", v.distortedPath, v.referencePath, err, stderr.String())
		return
	}

	// v.log.Info().Msgf("stdout: %s", stderr.String())

	// grep for VMAF score: 77.281242
	matches := vmafRE.FindStringSubmatch(stderr.String())
	if len(matches) != 3 {
		err = fmt.Errorf("failed to parse vmaf score from ffmpeg output")
		return
	}
	score, err = strconv.ParseFloat(matches[1], 64)

	return
}
