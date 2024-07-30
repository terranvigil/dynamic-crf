package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/model"
)

const vmafREString = "VMAF score: ((\\d*[.])?\\d+)"

var vmafRE = regexp.MustCompile(vmafREString)

// FfmpegVMAF will calculate the VMAF score of a distorted video compared
// to a reference video
type FfmpegVMAF struct {
	logger        zerolog.Logger
	referencePath string
	distortedPath string
	// 1-10, 1 is slowest, 10 is fastest, tradeoff between speed and accuracy
	speed int
}

func NewFfmpegVMAF(logger zerolog.Logger, distorted string, reference string, speed int) *FfmpegVMAF {
	return &FfmpegVMAF{
		logger:        logger,
		distortedPath: distorted,
		referencePath: reference,
		speed:         speed,
	}
}

// TODO finished #1, need to implement 2-8

// Requirements:
//  1. Distorted and reference must have the same resolution, scale distorted if not
//  2. Distorted and reference must have the same framerate, resample distorted if not
//  3. Distorted and reference must have the same duration, trim the longer of the two if not
//  4. Distorted and reference must have the same color space, convert distorted if not
//  5. Distorted and reference must have the same color range, convert distorted if not
//  6. Distorted and reference must both be progressive or both be interlaced, convert either if not
//  7. Distorted and reference must have the same pixel aspect ratio, convert distorted if not
//  8. Scaling algorithm used to scale reference should be the same used to scale the distorted
//     when it was originally encoded
func (v *FfmpegVMAF) Run(ctx context.Context) (float64, error) {
	var err error
	var stderr bytes.Buffer

	// get reference an distorted metadata
	var refMeta, distMeta *model.MediaInfo
	if refMeta, err = NewMediaInfo(v.logger, v.referencePath).Run(ctx); err != nil {
		return 0, fmt.Errorf("failed to get mediainfo of reference, err: %w", err)
	}
	if distMeta, err = NewMediaInfo(v.logger, v.distortedPath).Run(ctx); err != nil {
		return 0, fmt.Errorf("failed to get mediainfo of distorted, err: %w", err)
	}

	var refW, refH, distW, distH int
	if len(refMeta.GetVideoTracks()) == 0 {
		return 0, errors.New("reference has no video tracks")
	}
	if len(distMeta.GetVideoTracks()) == 0 {
		return 0, errors.New("distorted has no video tracks")
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
		"-i", v.referencePath,
		"-i", v.distortedPath,
		"-an",
	}

	scale := ""
	vmaf := fmt.Sprintf("libvmaf=n_threads=%d:n_subsample=%d", threads, v.speed)
	if refW != distW || refH != distH {
		v.logger.Info().Msgf("distorted and reference have different resolutions, upscaling distorted: %d:%d -> %d:%d", distW, distH, refW, refH)
		// Note use Lanczos or Spline for sampling down, Bicubic or Lanczos for sampling up
		// Note Lanczos is sharper but comes at the cost of ringing artifacts
		// Note there may be cases where it is better to scale the reference to the distorted
		//   resolution, e.g. the referende is 4K and the distorted is 1080p
		scale = fmt.Sprintf("scale=%d:%d:flags=bicubic,", refW, refH)
	}

	vmafFilter := "[0:v]setpts=PTS-STARTPTS[reference];"
	vmafFilter += fmt.Sprintf("[1:v]%ssetpts=PTS-STARTPTS[distorted];", scale)
	vmafFilter += "[distorted][reference]" + vmaf
	args = append(args, "-lavfi", vmafFilter, "-f", "null", "-")

	v.logger.Info().Msgf("running ffmpeg vmaf with distorted: %s, reference: %s", v.distortedPath, v.referencePath)
	v.logger.Info().Msgf("ffmpeg args: %v", args)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffmpeg vmaf of distorted: %s, reference: %s failed, err: %w, message: %s", v.distortedPath, v.referencePath, err, stderr.String())
	}

	// TODO use json for output and then marshal into struct
	// grep for VMAF score: 77.281242
	matches := vmafRE.FindStringSubmatch(stderr.String())
	if len(matches) != 3 { //nolint:mnd
		return 0, errors.New("failed to parse vmaf score from ffmpeg output")
	}
	return strconv.ParseFloat(matches[1], 64)
}
