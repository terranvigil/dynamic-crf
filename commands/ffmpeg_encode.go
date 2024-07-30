package commands

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/rs/zerolog"
)

// FfmpegEncode performs a simple encode given encoding parameters
type FfmpegEncode struct {
	logger     zerolog.Logger
	cfg        TranscodeConfig
	sourcePath string
	targetPath string
}

func NewFfmpegEncode(logger zerolog.Logger, source string, target string, cfg TranscodeConfig) *FfmpegEncode {
	return &FfmpegEncode{
		logger:     logger,
		sourcePath: source,
		targetPath: target,
		cfg:        cfg,
	}
}

func (e *FfmpegEncode) Run(ctx context.Context) error {
	var err error
	var stderr bytes.Buffer

	if e.cfg.VideoCRF > 0 {
		e.logger.Info().Msgf("running ffmpeg test encode with crf: %d", e.cfg.VideoCRF)
	} else if e.cfg.VideoBitrateKbps > 0 {
		e.logger.Info().Msgf("running ffmpeg test encode with video kbps: %d", e.cfg.VideoBitrateKbps)
	} else {
		e.logger.Fatal().Msg("bitrate or crf required for encode action")
	}

	args := []string{
		"-hide_banner",
		"-i", e.sourcePath,
		"-y",
	}
	if e.cfg.VideoCodec != "" {
		args = append(args, "-c:v", e.cfg.VideoCodec)
		if e.cfg.VideoBitrateKbps != 0 {
			args = append(args, "-b:v", fmt.Sprintf("%dk", e.cfg.VideoBitrateKbps))
		} else if e.cfg.VideoCRF != 0 {
			args = append(args, "-crf", strconv.Itoa(e.cfg.VideoCRF))
		}
		if e.cfg.VideoMaxBitrateKbps != 0 {
			args = append(args, "-maxrate", fmt.Sprintf("%dk", e.cfg.VideoMaxBitrateKbps))
		}
		if e.cfg.VideoMinBitrateKbps != 0 {
			args = append(args, "-minrate", fmt.Sprintf("%dk", e.cfg.VideoMinBitrateKbps))
		}
		if e.cfg.VideoBufferSizeKbps != 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", e.cfg.VideoBufferSizeKbps))
		}
		if e.cfg.FPSNumerator != 0 && e.cfg.FPSDenominator != 0 {
			args = append(args, "-r", fmt.Sprintf("%d/%d", e.cfg.FPSNumerator, e.cfg.FPSDenominator))
		}

		if e.cfg.Width != 0 || e.cfg.Height != 0 {
			// preserve aspect ratio if only one dimension is set, additionally use multiples of 2
			// for codec compatibility
			// TODO check what the dimensions will be chosen by ffmpeg as we may be able to skip
			//   this step if they are equivalent and the source is not anymorphic
			if e.cfg.Width == 0 {
				e.cfg.Width = -2 //nolint:mnd
			} else if e.cfg.Height == 0 {
				e.cfg.Height = -2 //nolint:mnd
			}
			args = append(args, "-filter:v", fmt.Sprintf("[in]scale=%d:%d:flags=lanczos[out]", e.cfg.Width, e.cfg.Height))
		}
		if e.cfg.Tune != "" {
			args = append(args, "-tune", e.cfg.Tune)
		}
		// TODO if we're trying to get an accurate VMAF score, disable psycho-visual optimizations since
		//   they increase the difference between source and output in order to improve perceived quality
		// -tune psnr
	} else {
		args = append(args, "-vn")
	}
	if e.cfg.AudioCodec != "" {
		args = append(args, "-c:a", e.cfg.AudioCodec)
		if e.cfg.AudioBitrateKbps != 0 {
			args = append(args, "-b:a", fmt.Sprintf("%dk", e.cfg.AudioBitrateKbps))
		}
	} else {
		args = append(args, "-an")
	}
	args = append(args, e.targetPath)

	e.logger.Info().Msgf("ffmpeg args: %v", args)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg encode of %s failed, err: %w, message: %s", e.sourcePath, err, stderr.String())
	}

	return nil
}
