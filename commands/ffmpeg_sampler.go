package commands

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/model"
)

type FfmpegSampler struct {
	log              zerolog.Logger
	sourcePath       string
	sampleOutputPath string
	fps              float64
	scenes           []model.Scene
	// cfg *TranscodeConfig
}

func NewFfmpegSampler(log zerolog.Logger, sourcePath string, samplePath string, fps float64, scenes []model.Scene) *FfmpegSampler {
	return &FfmpegSampler{
		log:              log,
		sourcePath:       sourcePath,
		sampleOutputPath: samplePath,
		fps:              fps,
		scenes:           scenes,
		// cfg: cfg,
	}
}

func (s *FfmpegSampler) Run(ctx context.Context) error {
	var err error
	var stderr bytes.Buffer

	/*
		args := []string{
			"-i", "/Users/terran.vigil/go/src/github.com/terranvigil/dynamic-crf/fixtures/media/perseverance_1280.mv4",
			"-an",
			"-c:v", "copy",
			"-filter_complex",
			"[0:v]trim=start_pts=196096:end_pts=200000,setpts=PTS-STARTPTS[v0];[0:v]trim=start_pts=210000:end_pts=2030000,setpts=PTS-STARTPTS[v1];[v0][v1]concat=n=2:v=1[out]",
			"-map", "[out]",
			"-y", "/Users/terran.vigil/go/src/github.com/terranvigil/dynamic-crf/testo.mp4",
		}
	*/

	totalDur := func() float64 {
		var total float64
		for _, scene := range s.scenes {
			total += scene.Duration
		}
		return total
	}()
	s.log.Info().Msgf("running ffmpeg sampler, creating sample of %fs duration", totalDur)
	tempPaths := []string{}

	for i, scene := range s.scenes {
		s.log.Info().Msgf("scene #%d:, start: %fs, dur: %fs", i+1, scene.StartPTSSec, scene.Duration)

		f, err := os.CreateTemp("", "smpl_*.ts")
		if err != nil {
			return fmt.Errorf("failed to create temp file for encoding sample, err: %w, message: %s", err, stderr.String())
		}
		defer os.Remove(f.Name())
		tempPaths = append(tempPaths, f.Name())

		args := []string{
			"-hide_banner",
			// TODO add this back in to target keyframe before start time
			//"-ss", strconv.Itoa(r[0]),
			"-i", s.sourcePath,
			"-ss", fmt.Sprintf("%f", scene.StartPTSSec),
			"-frames:v", fmt.Sprintf("%d", int(s.fps*scene.Duration)),
			"-c:v", "copy",
			"-an",
			"-y", f.Name(),
		}
		s.log.Debug().Msgf("ffmpeg args: %v", args)

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
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg sample concat %s failed, err: %w, message: %s", concat, err, stderr.String())
	}

	return nil
}
