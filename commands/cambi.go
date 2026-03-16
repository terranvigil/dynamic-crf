package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"syscall"

	"github.com/google/uuid"

	"github.com/terranvigil/dynamic-crf/model"
)

// max="18.491441" mean="0.301510"
const cambiREString = "max=\"(\\d+\\.\\d+)\".*mean=\"(\\d+\\.\\d+)\""

var cambiRE = regexp.MustCompile(cambiREString)

type Cambi struct {
	logger        *slog.Logger
	referencePath string
	distortedPath string
}

func NewCambi(logger *slog.Logger, distorted string, reference string) *Cambi {
	return &Cambi{
		logger:        logger,
		distortedPath: distorted,
		referencePath: reference,
	}
}

func (c *Cambi) Run(ctx context.Context) (float64, float64, error) {
	var stderr, stdout bytes.Buffer

	var refMeta, distMeta *model.MediaInfo
	var err error
	if refMeta, err = NewMediaInfo(c.logger, c.referencePath).Run(ctx); err != nil {
		return 0, 0, fmt.Errorf("failed to get mediainfo of reference, err: %w", err)
	}
	if distMeta, err = NewMediaInfo(c.logger, c.distortedPath).Run(ctx); err != nil {
		return 0, 0, fmt.Errorf("failed to get mediainfo of distorted, err: %w", err)
	}

	if len(refMeta.GetVideoTracks()) == 0 {
		return 0, 0, errors.New("reference has no video tracks")
	}
	if len(distMeta.GetVideoTracks()) == 0 {
		return 0, 0, errors.New("distorted has no video tracks")
	}
	distW := distMeta.GetVideoTracks()[0].Width
	distH := distMeta.GetVideoTracks()[0].Height

	pipe1Path := os.TempDir() + "pipe1_" + uuid.New().String() + ".yuv"
	pipe2Path := os.TempDir() + "pipe2_" + uuid.New().String() + ".yuv"
	if err := syscall.Mkfifo(pipe1Path, 0o666); err != nil {
		return 0, 0, fmt.Errorf("failed to create named pipe: %w", err)
	}
	defer os.Remove(pipe1Path) //nolint:errcheck
	if err := syscall.Mkfifo(pipe2Path, 0o666); err != nil {
		return 0, 0, fmt.Errorf("failed to create named pipe: %w", err)
	}
	defer os.Remove(pipe2Path) //nolint:errcheck

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

	c.logger.Info("running cambi", "distorted", c.distortedPath, "reference", c.referencePath)
	c.logger.Debug("cambi", "args", args)

	var (
		decodeErrs [2]error
		wg         sync.WaitGroup
	)
	wg.Add(2) //nolint:mnd
	go func() {
		defer wg.Done()
		decodeErrs[0] = c.decode(ctx, c.distortedPath, pipe1Path)
	}()
	go func() {
		defer wg.Done()
		decodeErrs[1] = c.decode(ctx, c.distortedPath, pipe2Path)
	}()

	cmd := exec.CommandContext(ctx, "vmaf", args...)
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	if err = cmd.Run(); err != nil {
		wg.Wait()
		return 0, 0, fmt.Errorf("cambi of distorted: %s, reference: %s failed, err: %w, message: %s", c.distortedPath, c.referencePath, err, stderr.String())
	}

	wg.Wait()
	for _, derr := range decodeErrs {
		if derr != nil {
			return 0, 0, fmt.Errorf("decode for cambi failed: %w", derr)
		}
	}

	matches := cambiRE.FindStringSubmatch(stdout.String())
	if len(matches) != 3 { //nolint:mnd
		return 0, 0, errors.New("failed to parse cambi score from output")
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

func (c *Cambi) decode(ctx context.Context, sourcePath, pipePath string) error {
	var stderr bytes.Buffer

	args := []string{
		"-i", sourcePath,
		"-c:v", "rawvideo",
		"-pix_fmt", "yuv420p",
		"-y", pipePath,
	}

	c.logger.Debug("running ffmpeg decode to pipe", "pipe", pipePath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg decode to pipe %s failed, err: %w, message: %s", pipePath, err, stderr.String())
	}
	return nil
}
