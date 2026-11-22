package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"

	"github.com/terranvigil/dynamic-crf/model"
)

type FfprobeScenes struct {
	sourcePath  string
	streamIndex int
	log         *slog.Logger
}

// NewFfprobeScenes detects scene cuts in the stream at streamIndex, which
// should be the one encodes read (-map 0:V:0); movie= alone picks ffmpeg's
// "best" video stream.
func NewFfprobeScenes(logger *slog.Logger, sourcePath string, streamIndex int) *FfprobeScenes {
	return &FfprobeScenes{
		sourcePath:  sourcePath,
		streamIndex: streamIndex,
		log:         logger,
	}
}

func (f *FfprobeScenes) Run(ctx context.Context) (*model.FfprobeFrames, error) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	args := []string{
		"-hide_banner",
		"-v", "error",
		"-show_frames",
		"-f", "lavfi", "movie=" + escapeFilterValue(f.sourcePath) + ":s=" + strconv.Itoa(f.streamIndex) + ",select=gt(scene\\,0.3)",
		"-of", "json",
	}

	f.log.Info("running ffprobe scene detection", "path", f.sourcePath)

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe scene detection for %s failed, err: %w, message: %s", f.sourcePath, err, stderr.String())
	}

	response := &model.FfprobeFrames{}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("ffprobe scene detection for %s failed, unmarshall: err: %w", f.sourcePath, err)
	}

	return response, nil
}
