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

type FfprobeScenes struct {
	source *os.File
	log    zerolog.Logger
}

func NewFfprobeScenes(logger zerolog.Logger, source *os.File) *FfprobeScenes {
	return &FfprobeScenes{
		source: source,
		log:    logger,
	}
}

func (f *FfprobeScenes) Run(ctx context.Context) (*model.FfprobeFrames, error) {
	var err error
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	args := []string{
		"-hide_banner",
		"-v", "error",
		"-show_frames",
		"-f", "lavfi", "movie=" + f.source.Name() + ",select=gt(scene\\,0.3)",
		"-of", "json",
	}

	f.log.Info().Msg("running ffprobe scene detection")

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe scene detection for %s failed, err: %w, message: %s", f.source.Name(), err, stderr.String())
	}

	response := &model.FfprobeFrames{}
	if err = json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("ffprobe scene detection for %s failed, unmarshall: err: %w", f.source.Name(), err)
	}

	return response, nil
}
