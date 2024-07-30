package actions

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/model"
)

const (
	DefaultSpeed = 5
)

// VMAFScore will encode a sample of the source video with a given trancode configuration
// and return the VMAF score
type VMAFScore struct {
	logger          zerolog.Logger
	transcodeConfig commands.TranscodeConfig
	referencePath   string
}

func NewVMAFScore(logger zerolog.Logger, cfg commands.TranscodeConfig, referencePath string) *VMAFScore {
	return &VMAFScore{
		logger:          logger,
		referencePath:   referencePath,
		transcodeConfig: cfg,
	}
}

func (v *VMAFScore) Run(ctx context.Context) (score float64, averageBitrateKBPS int, maxBitrateKBPS int, streamSizeKB int, err error) {
	var testEncode *os.File
	if testEncode, err = os.CreateTemp("", "tst_encode*.mp4"); err != nil {
		err = fmt.Errorf("failed to create temp target encode file, err: %w", err)
		return
	}
	defer os.Remove(testEncode.Name())

	if err = commands.NewFfmpegEncode(v.logger, v.referencePath, testEncode.Name(), v.transcodeConfig).Run(ctx); err != nil {
		err = fmt.Errorf("failed to encode reference: %s, err: %w", v.referencePath, err)
		return
	}

	if score, err = commands.NewFfmpegVMAF(v.logger, testEncode.Name(), v.referencePath, DefaultSpeed).Run(ctx); err != nil {
		err = fmt.Errorf("failed to calc vmaf of test output, err: %w", err)
		return
	}

	var metadata *model.MediaInfo
	if metadata, err = commands.NewMediaInfo(v.logger, testEncode.Name()).Run(ctx); err != nil {
		err = fmt.Errorf("failed to get mediainfo of test output, err: %w", err)
		return
	}

	averageBitrateKBPS = metadata.GetVideoTracks()[0].BitRate / 1000    //nolint:mnd
	maxBitrateKBPS = metadata.GetVideoTracks()[0].BitRateMaximum / 1000 //nolint:mnd
	streamSizeKB = metadata.GetVideoTracks()[0].StreamSize / 1000       //nolint:mnd

	return
}
