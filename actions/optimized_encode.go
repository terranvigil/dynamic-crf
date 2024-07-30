package actions

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/model"
)

type OptimizedEncoded struct {
	logger          zerolog.Logger
	transcodeConfig commands.TranscodeConfig
	targetVMAF      float64
	crfInitial      int
	crfMin          int
	crfMax          int
	tolerance       float64
	sourcePath      string
	targetPath      string
}

func NewOptimizedEncoded(logger zerolog.Logger, cfg commands.TranscodeConfig, source string, target string, targetVMAF float64, crfInitial int, crfMin int, crfMax int, tolerance float64) *OptimizedEncoded {
	return &OptimizedEncoded{
		logger:          logger,
		transcodeConfig: cfg,
		targetVMAF:      targetVMAF,
		crfInitial:      crfInitial,
		crfMin:          crfMin,
		crfMax:          crfMax,
		tolerance:       tolerance,
		sourcePath:      source,
		targetPath:      target,
	}
}

func (e *OptimizedEncoded) Run(ctx context.Context) error {
	// search for optimized crf using a target vmaf
	var err error
	var crf int
	var vmaf float64
	search := NewCrfSearch(e.logger, e.sourcePath, e.targetVMAF, e.crfInitial, e.crfMin, e.crfMax, e.tolerance, e.transcodeConfig)
	if crf, vmaf, err = search.Run(ctx); err != nil {
		e.logger.Fatal().Err(err).Msg("failed to run crf search")
	}
	e.logger.Info().Msgf("Done: Found crf: %d, vmaf: %.2f", crf, vmaf)

	e.transcodeConfig.VideoCRF = crf
	if err = commands.NewFfmpegEncode(e.logger, e.sourcePath, e.targetPath, e.transcodeConfig).Run(ctx); err != nil {
		return err
	}

	if vmaf, err = commands.NewFfmpegVMAF(e.logger, e.targetPath, e.sourcePath, DefaultSpeed).Run(ctx); err != nil {
		return err
	}

	var metadata *model.MediaInfo
	if metadata, err = commands.NewMediaInfo(e.logger, e.targetPath).Run(ctx); err != nil {
		return fmt.Errorf("failed to get mediainfo of test output, err: %w", err)
	}

	averageBitrateKBPS := metadata.GetVideoTracks()[0].BitRate / 1000 //nolint:mnd
	streamSizeMB := metadata.GetVideoTracks()[0].StreamSize / 1000000 //nolint:mnd

	e.logger.Info().Msgf("Done: Optimized encode with crf: %d, score: %.2f, avg bitrate: %dKbps, stream size: %dM", crf, vmaf, averageBitrateKBPS, streamSizeMB)

	return nil
}
