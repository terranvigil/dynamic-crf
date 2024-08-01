package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"syscall"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/model"
)

// max="18.491441" mean="0.301510"
const cambiREString = "max=\"(\\d+\\.\\d+)\".*mean=\"(\\d+\\.\\d+)\""

var cambiRE = regexp.MustCompile(cambiREString)

type Cambi struct {
	logger        zerolog.Logger
	referencePath string
	distortedPath string
}

func NewCambi(logger zerolog.Logger, distorted string, reference string) *Cambi {
	return &Cambi{
		logger:        logger,
		distortedPath: distorted,
		referencePath: reference,
	}
}

func (c *Cambi) Run(ctx context.Context) (float64, float64, error) {
	var err error
	var stderr, stdout bytes.Buffer

	// get reference an distorted metadata
	var refMeta, distMeta *model.MediaInfo
	if refMeta, err = NewMediaInfo(c.logger, c.referencePath).Run(ctx); err != nil {
		return 0, 0, fmt.Errorf("failed to get mediainfo of reference, err: %w", err)
	}
	if distMeta, err = NewMediaInfo(c.logger, c.distortedPath).Run(ctx); err != nil {
		return 0, 0, fmt.Errorf("failed to get mediainfo of distorted, err: %w", err)
	}

	var /*refW, refH,*/ distW, distH int
	if len(refMeta.GetVideoTracks()) == 0 {
		return 0, 0, errors.New("reference has no video tracks")
	}
	if len(distMeta.GetVideoTracks()) == 0 {
		return 0, 0, errors.New("distorted has no video tracks")
	}
	//nolint:gocritic
	//	refW = refMeta.GetVideoTracks()[0].Width
	//	refH = refMeta.GetVideoTracks()[0].Height
	distW = distMeta.GetVideoTracks()[0].Width
	distH = distMeta.GetVideoTracks()[0].Height

	pipe1Path := os.TempDir() + "pipe1_" + uuid.New().String() + ".yuv"
	pipe2Path := os.TempDir() + "pipe2_" + uuid.New().String() + ".yuv"
	if err := syscall.Mkfifo(pipe1Path, 0o666); err != nil {
		c.logger.Fatal().Err(err).Msg("make named pipe file failed")
	}
	defer os.Remove(pipe1Path)
	if err := syscall.Mkfifo(pipe2Path, 0o666); err != nil {
		c.logger.Fatal().Err(err).Msg("make named pipe file failed")
	}
	defer os.Remove(pipe2Path)

	// TODO support 'full_ref=true' for full reference

	args := []string{
		"--reference", pipe1Path,
		"--distorted", pipe2Path,
		"--width", strconv.Itoa(distW),
		"--height", strconv.Itoa(distH),
		"--pixel_format", "420",
		"--bitdepth", "8",
		"--no_prediction",
		"--feature", "cambi",
		"--output", "/dev/stdout",
	}

	c.logger.Info().Msgf("running cambi with distorted: %s, reference: %s", c.distortedPath, c.referencePath)
	c.logger.Info().Msgf("cambi args: %v", args)

	go c.decode(ctx, c.distortedPath, pipe1Path)
	go c.decode(ctx, c.distortedPath, pipe2Path)

	cmd := exec.CommandContext(ctx, "vmaf", args...)
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	if err = cmd.Run(); err != nil {
		return 0, 0, fmt.Errorf("cambi of distorted: %s, reference: %s failed, err: %w, message: %s", c.distortedPath, c.referencePath, err, stderr.String())
	}

	// TODO use json for output and then marshal into struct
	// grep for CAMBI scores: max="18.491441" mean="0.301510"
	matches := cambiRE.FindStringSubmatch(stdout.String())
	if len(matches) != 3 { //nolint:mnd
		return 0, 0, errors.New("failed to parse cambi score from ffmpeg output")
	}

	var max, mean float64
	if max, err = strconv.ParseFloat(matches[1], 64); err != nil {
		return 0, 0, fmt.Errorf("failed to parse max cambi score, err: %w", err)
	}
	if mean, err = strconv.ParseFloat(matches[2], 64); err != nil {
		return 0, 0, fmt.Errorf("failed to parse mean cambi score, err: %w", err)
	}

	return max, mean, nil
}

func (c *Cambi) decode(ctx context.Context, sourcePath, pipePath string) {
	var err error
	var stderr bytes.Buffer

	args := []string{
		"-i", sourcePath,
		"-c:v", "rawvideo",
		"-pix_fmt", "yuv420p",
		"-y", pipePath,
	}

	c.logger.Info().Msgf("running ffmpeg decode to pipe: %s", pipePath)
	c.logger.Info().Msgf("ffmpeg decode args: %v", args)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if err = cmd.Run(); err != nil {
		c.logger.Fatal().Err(err).Msgf("ffmpeg decode of pipe: %s failed, err: %v, message: %s", pipePath, err, stderr.String())
	}
}
