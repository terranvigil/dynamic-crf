package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
)

// FfmpegEncode performs a simple encode given encoding parameters
type FfmpegEncode struct {
	logger     *slog.Logger
	cfg        TranscodeConfig
	sourcePath string
	targetPath string
}

func NewFfmpegEncode(logger *slog.Logger, source string, target string, cfg TranscodeConfig) *FfmpegEncode {
	return &FfmpegEncode{
		logger:     logger,
		sourcePath: source,
		targetPath: target,
		cfg:        cfg,
	}
}

// codecs where -crf alone is capped by the default target bitrate and
// constant quality needs -b:v 0
var crfNeedsZeroBitrate = map[string]bool{
	"libvpx":     true,
	"libvpx-vp9": true,
	"libaom-av1": true,
}

func (e *FfmpegEncode) Run(ctx context.Context) error {
	var stderr bytes.Buffer

	if err := checkDistinct(e.sourcePath, e.targetPath); err != nil {
		return err
	}
	target, err := ResolveOutput(e.targetPath)
	if err != nil {
		return err
	}
	// write beside the target and rename on success, so a failed or
	// interrupted encode never leaves a partial file at -o or clobbers one
	partial, err := createPartial(target)
	if err != nil {
		return fmt.Errorf("create partial output: %w", err)
	}
	defer os.Remove(partial) //nolint:errcheck
	args, err := e.argsFor(partial)
	if err != nil {
		return err
	}

	if e.cfg.VideoCRF > 0 {
		e.logger.Info("running ffmpeg encode", "crf", FormatCRF(e.cfg.VideoCRF))
	} else {
		e.logger.Info("running ffmpeg encode", "bitrateKbps", e.cfg.VideoBitrateKbps)
	}
	e.logger.Debug("ffmpeg encode", "args", args)

	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg encode of %s failed, err: %w, message: %s", e.sourcePath, err, tail(stderr.String()))
	}
	if err := os.Rename(partial, target); err != nil {
		return fmt.Errorf("move encode into place: %w", err)
	}
	return nil
}

// ResolveOutput follows a symlink at path, even a dangling one, so the
// encode writes through the link rather than replacing it.
func ResolveOutput(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	// dangling: follow the chain by hand, bounded in case it loops
	cur := path
	for range 40 {
		link, err := os.Readlink(cur)
		if err != nil {
			return cur, nil
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(cur), link)
		}
		cur = link
	}
	return "", fmt.Errorf("output %s is a symlink loop", path)
}

// createPartial reserves a unique name beside target. Unique so concurrent
// runs to one -o can't share it; created 0666 so the umask, not a temp-file
// default of 0600, sets the final file's mode.
func createPartial(target string) (string, error) {
	dir, base := filepath.Dir(target), filepath.Base(target)
	for range 100 {
		name := filepath.Join(dir, fmt.Sprintf(".partial-%d-%s", rand.Uint64(), base))
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666) //nolint:gosec
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return name, f.Close()
	}
	return "", errors.New("could not pick a unique partial file name")
}

// checkDistinct refuses to encode a file over itself. ffmpeg only compares
// the path strings, so ./in.mp4 for in.mp4 gets past it and truncates the source.
func checkDistinct(source, target string) error {
	si, err := os.Stat(source)
	if err != nil {
		return nil //nolint:nilerr // ffmpeg reports a missing source better
	}
	if ti, err := os.Stat(target); err == nil && os.SameFile(si, ti) {
		return fmt.Errorf("output %s is the same file as input %s", target, source)
	}
	return nil
}

func (e *FfmpegEncode) args() ([]string, error) {
	return e.argsFor(e.targetPath)
}

func (e *FfmpegEncode) argsFor(out string) ([]string, error) {
	cfg := e.cfg
	if cfg.VideoCRF <= 0 && cfg.VideoBitrateKbps <= 0 {
		return nil, fmt.Errorf("bitrate or crf required for encode")
	}
	if cfg.VideoCodec == "" {
		return nil, fmt.Errorf("video codec required for encode")
	}

	args := []string{
		"-hide_banner",
		"-nostdin",
		"-i", e.sourcePath,
		"-y",
		"-map", "0:V:0",
		"-c:v", cfg.VideoCodec,
	}
	audio := cfg.AudioCodec != "" && cfg.AudioCodec != AudioNone
	if audio {
		// optional so sources without audio still encode
		args = append(args, "-map", "0:a?")
	}
	if cfg.Preset != "" {
		args = append(args, "-preset", cfg.Preset)
	}
	if cfg.VideoBitrateKbps > 0 {
		args = append(args, "-b:v", fmt.Sprintf("%dk", cfg.VideoBitrateKbps))
	} else {
		args = append(args, "-crf", FormatCRF(cfg.VideoCRF))
		if crfNeedsZeroBitrate[cfg.VideoCodec] {
			args = append(args, "-b:v", "0")
		}
	}
	if cfg.VideoMaxBitrateKbps > 0 {
		args = append(args, "-maxrate", fmt.Sprintf("%dk", cfg.VideoMaxBitrateKbps))
	}
	if cfg.VideoMinBitrateKbps > 0 {
		args = append(args, "-minrate", fmt.Sprintf("%dk", cfg.VideoMinBitrateKbps))
	}
	if cfg.VideoBufferSizeKbps > 0 {
		args = append(args, "-bufsize", fmt.Sprintf("%dk", cfg.VideoBufferSizeKbps))
	}
	if cfg.FPSNumerator > 0 && cfg.FPSDenominator > 0 {
		args = append(args, "-r", fmt.Sprintf("%d/%d", cfg.FPSNumerator, cfg.FPSDenominator))
	} else {
		// encode every source frame with its own timestamp. mp4's default cfr
		// mode drops frames from a variable-rate source whose header claims a
		// lower rate, and then the encode no longer matches its reference.
		args = append(args, "-fps_mode", "passthrough")
	}
	if cfg.Width > 0 || cfg.Height > 0 {
		// -2 keeps the aspect ratio for the unset side, rounded to a multiple of 2
		w, h := cfg.Width, cfg.Height
		if w == 0 {
			w = -2
		} else if h == 0 {
			h = -2
		}
		args = append(args, "-vf", fmt.Sprintf("scale=%d:%d:flags=lanczos", w, h))
	}
	if cfg.PixFmt != "" {
		args = append(args, "-pix_fmt", cfg.PixFmt)
	}
	if cfg.Tune != "" {
		args = append(args, "-tune", cfg.Tune)
	}
	if audio {
		args = append(args, "-c:a", cfg.AudioCodec)
		if cfg.AudioBitrateKbps > 0 {
			args = append(args, "-b:a", fmt.Sprintf("%dk", cfg.AudioBitrateKbps))
		}
	} else {
		args = append(args, "-an")
	}
	args = append(args, "-sn", "-dn", safeOutPath(out))
	return args, nil
}
