package commands

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// CheckAudioMux muxes half a second of the source's audio the way the final
// encode will, so an audio codec mp4 can't hold (pcm_mulaw, wma) fails before
// a search instead of after it.
func CheckAudioMux(ctx context.Context, logger *slog.Logger, source string, cfg TranscodeConfig) error {
	if cfg.AudioCodec == "" || cfg.AudioCodec == AudioNone {
		return nil
	}
	meta, err := NewFfprobeMetadata(logger, source).Run(ctx)
	if err != nil {
		return err
	}
	if meta.AudioStream() == nil {
		return nil
	}
	dir, err := os.MkdirTemp("", "dcrf_audiocheck_*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir) //nolint:errcheck

	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-i", source, "-map", "0:a?", "-vn", "-c:a", cfg.AudioCodec}
	if cfg.AudioBitrateKbps > 0 {
		args = append(args, "-b:a", fmt.Sprintf("%dk", cfg.AudioBitrateKbps))
	}
	args = append(args, "-t", "0.5", "-y", filepath.Join(dir, "check.mp4"))
	logger.Debug("audio mux check", "args", args)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("audio can't go into the mp4 with -acodec %s (try -acodec aac or none): %w: %s", cfg.AudioCodec, err, tail(stderr.String()))
	}
	return nil
}
