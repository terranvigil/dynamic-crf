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

type MediaInfo struct {
	sourcePath string
	log        *slog.Logger
}

func NewMediaInfo(logger *slog.Logger, sourcePath string) *MediaInfo {
	return &MediaInfo{
		sourcePath: sourcePath,
		log:        logger,
	}
}

func (m *MediaInfo) Run(ctx context.Context) (*model.MediaInfo, error) {
	var out bytes.Buffer
	var stderr bytes.Buffer

	args := []string{"-F", "--Output=JSON", m.sourcePath}
	cmd := exec.CommandContext(ctx, "mediainfo", args...)
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("mediainfo inspection for %s failed, err: %w, message: %s", m.sourcePath, err, stderr.String())
	}

	response := model.MediaInfo{}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("mediainfo inspection for %s failed, unmarshall: err: %w", m.sourcePath, err)
	}

	return &response, nil
}
