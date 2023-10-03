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

type FfprobeKeyframes struct {
	source *os.File
	log    zerolog.Logger
}

func NewFfprobeKeyframes(logger zerolog.Logger, source *os.File) *FfprobeKeyframes {
	return &FfprobeKeyframes{
		log:    logger,
		source: source,
	}
}

func (f *FfprobeKeyframes) Run(ctx context.Context) (*model.FfprobeFrames, error) {
	var err error
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	args := []string{
		"-hide_banner",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_frames",
		"-skip_frame",
		"nokey",
		"-of", "json",
		"-f", "lavfi", fmt.Sprintf("movie=%s,select=gt(scene\\,.4)", f.source.Name()),
	}

	f.log.Info().Msg("running ffprobe keyframe inspection")

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe keyframe inspection for %s failed, err: %w, message: %s", f.source.Name(), err, stderr.String())
	}

	response := model.FfprobeFrames{}
	if err = json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("ffprobe keyframe inspection for %s failed, unmarshall: err: %w", f.source.Name(), err)
	}

	return &response, nil
}
