package actions

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/model"
)

const (
	DefaultSpeed = 5
)

// VMAFEncodeScore will encode the source video with a given transcode configuration
// and return the VMAF score
type VMAFEncodeScore struct {
	logger          *slog.Logger
	transcodeConfig commands.TranscodeConfig
	referencePath   string
}

func NewVMAFEncodeScore(logger *slog.Logger, cfg commands.TranscodeConfig, referencePath string) *VMAFEncodeScore {
	return &VMAFEncodeScore{
		logger:          logger,
		referencePath:   referencePath,
		transcodeConfig: cfg,
	}
}

func (v *VMAFEncodeScore) Run(ctx context.Context) (score float64, averageBitrateKBPS int, maxBitrateKBPS int, streamSizeKB int, err error) {
	var testEncode *os.File
	if testEncode, err = os.CreateTemp("", "tst_encode*.mp4"); err != nil {
		err = fmt.Errorf("failed to create temp target encode file, err: %w", err)
		return
	}
	defer os.Remove(testEncode.Name()) //nolint:errcheck

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

	tracks := metadata.GetVideoTracks()
	if len(tracks) == 0 {
		err = fmt.Errorf("no video tracks in encoded output")
		return
	}

	averageBitrateKBPS = int(tracks[0].BitRate) / 1000    //nolint:mnd
	maxBitrateKBPS = int(tracks[0].BitRateMaximum) / 1000 //nolint:mnd
	streamSizeKB = int(tracks[0].StreamSize) / 1000       //nolint:mnd

	return
}
