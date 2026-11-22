package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/terranvigil/dynamic-crf/model"
)

type FfprobeMetadata struct {
	sourcePath string
	log        *slog.Logger
}

func NewFfprobeMetadata(logger *slog.Logger, sourcePath string) *FfprobeMetadata {
	return &FfprobeMetadata{
		sourcePath: sourcePath,
		log:        logger,
	}
}

func (f *FfprobeMetadata) Run(ctx context.Context) (*model.FfprobeMetadata, error) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	args := []string{
		"-hide_banner",
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-of", "json",
		"-i", f.sourcePath,
	}

	f.log.Debug("running ffprobe metadata inspection", "path", f.sourcePath)

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe inspection for %s failed, err: %w, message: %s", f.sourcePath, err, stderr.String())
	}

	response := model.FfprobeMetadata{}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("ffprobe inspection for %s failed, unmarshall: err: %w", f.sourcePath, err)
	}

	return &response, nil
}
