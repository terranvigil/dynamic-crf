package commands

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/terranvigil/dynamic-crf/model"
)

// SampleExt is the container the sampler writes. The sample is lossless FFV1,
// so any source codec and pixel format survives and scenes start exactly
// where they were detected rather than at the next keyframe.
const SampleExt = ".mkv"

// a sample shorter than this share of the requested frames lost scenes
const sampleMinFraction = 0.9

type FfmpegSampler struct {
	logger           *slog.Logger
	sourcePath       string
	sampleOutputPath string
	scenes           []model.Scene
}

func NewFfmpegSampler(log *slog.Logger, sourcePath string, samplePath string, scenes []model.Scene) *FfmpegSampler {
	return &FfmpegSampler{
		logger:           log,
		sourcePath:       sourcePath,
		sampleOutputPath: samplePath,
		scenes:           scenes,
	}
}

func (s *FfmpegSampler) Run(ctx context.Context) error {
	var totalDur float64
	for _, scene := range s.scenes {
		totalDur += scene.Duration
	}
	s.logger.Info("running ffmpeg sampler", "scenes", len(s.scenes), "sampleDuration", fmt.Sprintf("%.2fs", totalDur))

	dir, err := os.MkdirTemp("", "dcrf_sample_*")
	if err != nil {
		return fmt.Errorf("create sample temp dir: %w", err)
	}
	defer os.RemoveAll(dir) //nolint:errcheck

	var list strings.Builder
	for i, scene := range s.scenes {
		s.logger.Debug("extracting scene", "scene", i+1, "start", fmt.Sprintf("%.2fs", scene.StartPTSSec), "duration", fmt.Sprintf("%.2fs", scene.Duration))
		part := filepath.Join(dir, fmt.Sprintf("scene_%03d%s", i, SampleExt))
		if err := s.run(ctx, sceneArgs(s.sourcePath, part, scene)); err != nil {
			return fmt.Errorf("extract scene %d at %.2fs: %w", i+1, scene.StartPTSSec, err)
		}
		list.WriteString("file " + concatQuote(part) + "\n")
	}

	listPath := filepath.Join(dir, "scenes.txt")
	if err := os.WriteFile(listPath, []byte(list.String()), 0o600); err != nil {
		return fmt.Errorf("write concat list: %w", err)
	}
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", "-y", safeOutPath(s.sampleOutputPath)}
	if err := s.run(ctx, args); err != nil {
		return fmt.Errorf("concat scenes: %w", err)
	}
	return s.verify(ctx)
}

// verify catches parts that came out empty, e.g. a seek past the end, which
// concat skips without complaint.
func (s *FfmpegSampler) verify(ctx context.Context) error {
	var want float64
	for _, scene := range s.scenes {
		want += scene.Duration
	}
	info, err := ProbeVideo(ctx, s.logger, s.sampleOutputPath)
	if err != nil {
		return fmt.Errorf("probe sample: %w", err)
	}
	if info.Duration < sampleMinFraction*want {
		return fmt.Errorf("sample is %.1fs, expected %.1fs; some scenes came out empty", info.Duration, want)
	}
	return nil
}

func (s *FfmpegSampler) run(ctx context.Context, args []string) error {
	s.logger.Debug("ffmpeg sampler", "args", args)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, tail(stderr.String()))
	}
	return nil
}

// sceneArgs seeks on the input side while transcoding, which ffmpeg does
// frame-accurately, and writes intra-only FFV1 so the parts concat cleanly.
func sceneArgs(source, out string, scene model.Scene) []string {
	return []string{
		"-hide_banner", "-nostdin", "-v", "error",
		// 6 places so a 1001-denominator pts doesn't round past its frame
		"-ss", strconv.FormatFloat(scene.StartPTSSec, 'f', 6, 64),
		"-i", source,
		"-map", "0:V:0",
		// a duration, since a frame count is wrong on variable-rate sources
		"-t", strconv.FormatFloat(scene.Duration, 'f', 6, 64),
		// zero-based parts concat without gaps whatever the source's start time
		"-vf", "setpts=PTS-STARTPTS",
		"-c:v", "ffv1", "-level", "3", "-g", "1", "-slices", "16", "-slicecrc", "0",
		"-an", "-sn", "-dn",
		"-y", out,
	}
}

// concatQuote quotes a path for the concat demuxer's list file.
func concatQuote(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}
