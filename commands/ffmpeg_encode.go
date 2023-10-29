package commands

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"github.com/rs/zerolog"
)

// FfmpegEncode performs a simple encode given encoding parameters
type FfmpegEncode struct {
	log        zerolog.Logger
	cfg        TranscodeConfig
	sourcePath string
	targetPath string
}

func NewFfmpegEncode(log zerolog.Logger, source string, target string, cfg TranscodeConfig) *FfmpegEncode {
	return &FfmpegEncode{
		log:        log,
		sourcePath: source,
		targetPath: target,
		cfg:        cfg,
	}
}

func (e *FfmpegEncode) Run(ctx context.Context) error {
	var err error
	var stderr bytes.Buffer

	if e.cfg.VideoCRF > 0 {
		e.log.Info().Msgf("running ffmpeg test encode with crf: %d", e.cfg.VideoCRF)
	} else if e.cfg.VideoBitrateKbps > 0 {
		e.log.Info().Msgf("running ffmpeg test encode with video kbps: %d", e.cfg.VideoBitrateKbps)
	} else {
		e.log.Fatal().Msg("bitrate or crf required for encode action")
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
			args = append(args, "-crf", fmt.Sprintf("%d", e.cfg.VideoCRF))
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
				e.cfg.Width = -2
			} else if e.cfg.Height == 0 {
				e.cfg.Height = -2
			}
			args = append(args, "-vf", fmt.Sprintf("scale=%d:%d:flags=bicubic", e.cfg.Width, e.cfg.Height))
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

	// e.log.Debug().Msgf("ffmpeg args: %v", args)
	e.log.Info().Msgf("ffmepg args: %v", args)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg encode of %s failed, err: %w, message: %s", e.sourcePath, err, stderr.String())
	}

	return nil
}
