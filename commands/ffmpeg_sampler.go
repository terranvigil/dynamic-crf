package commands

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"

	"github.com/terranvigil/dynamic-crf/model"
)

type FfmpegSampler struct {
	logger           *slog.Logger
	sourcePath       string
	sampleOutputPath string
	fps              float64
	scenes           []model.Scene
}

func NewFfmpegSampler(log *slog.Logger, sourcePath string, samplePath string, fps float64, scenes []model.Scene) *FfmpegSampler {
	return &FfmpegSampler{
		logger:           log,
		sourcePath:       sourcePath,
		sampleOutputPath: samplePath,
		fps:              fps,
		scenes:           scenes,
	}
}

func (s *FfmpegSampler) Run(ctx context.Context) error {
	var stderr bytes.Buffer

	totalDur := func() float64 {
		var total float64
		for _, scene := range s.scenes {
			total += scene.Duration
		}
		return total
	}()
	s.logger.Info("running ffmpeg sampler", "sampleDuration", fmt.Sprintf("%.2fs", totalDur))

	// collect temp paths for cleanup after concat
	var tempPaths []string
	defer func() {
		for _, p := range tempPaths {
			os.Remove(p) //nolint:errcheck
		}
	}()

	for i, scene := range s.scenes {
		s.logger.Info("extracting scene", "scene", i+1, "start", fmt.Sprintf("%.2fs", scene.StartPTSSec), "duration", fmt.Sprintf("%.2fs", scene.Duration))

		f, err := os.CreateTemp("", "smpl_*.ts")
		if err != nil {
			return fmt.Errorf("failed to create temp file for scene sample: %w", err)
		}
		tempPaths = append(tempPaths, f.Name())

		args := []string{
			"-hide_banner",
			"-i", s.sourcePath,
			"-ss", fmt.Sprintf("%.2f", scene.StartPTSSec),
			"-frames:v", strconv.Itoa(int(s.fps * scene.Duration)),
			"-c:v", "copy",
			"-an",
			"-y", f.Name(),
		}
		s.logger.Debug("ffmpeg sample extract", "args", args)

		stderr.Reset()
		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		cmd.Stderr = &stderr
		if err = cmd.Run(); err != nil {
			return fmt.Errorf("ffmpeg sample creation %s failed, err: %w, message: %s", f.Name(), err, stderr.String())
		}
	}

	var concat string
	for _, path := range tempPaths {
		if concat == "" {
			concat = "concat:" + path
		} else {
			concat += "|" + path
		}
	}
	args := []string{
		"-hide_banner",
		"-i", concat,
		"-c:v", "copy",
		"-y", s.sampleOutputPath,
	}
	stderr.Reset()
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg sample concat %s failed, err: %w, message: %s", concat, err, stderr.String())
	}

	return nil
}
