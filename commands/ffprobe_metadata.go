package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/model"
)

type FfprobeMetadata struct {
	source *os.File
	log    zerolog.Logger
}

func NewFfprobeMetadata(logger zerolog.Logger, source *os.File) *FfprobeMetadata {
	return &FfprobeMetadata{
		source: source,
		log:    logger,
	}
}

func (f *FfprobeMetadata) Run(ctx context.Context) (*model.FfprobeMetadata, error) {
	var err error
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	args := []string{
		"-hide_banner",
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-of", "json",
		f.source.Name(),
	}

	f.log.Info().Msg("running ffprobe metadata inspection")

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe inspection for %s failed, err: %w, message: %s", f.source.Name(), err, stderr.String())
	}

	response := model.FfprobeMetadata{}
	if err = json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("ffprobe inspection for %s failed, unmarshall: err: %w", f.source.Name(), err)
	}

	return &response, nil
}
