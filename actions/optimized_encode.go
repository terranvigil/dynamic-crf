package actions

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/commands"
)

type OptimizedEncoded struct {
	logger          zerolog.Logger
	transcodeConfig commands.TranscodeConfig
	targetVMAF      float64
	sourcePath      string
	targetPath      string
}

func NewOptimizedEncoded(logger zerolog.Logger, cfg commands.TranscodeConfig, targetVMAF float64, source string, target string) *OptimizedEncoded {
	return &OptimizedEncoded{
		logger:          logger,
		transcodeConfig: cfg,
		targetVMAF:      targetVMAF,
		sourcePath:      source,
		targetPath:      target,
	}
}

func (e *OptimizedEncoded) Run(ctx context.Context) error {
	// search for optimized crf using a target vmaf
	var err error
	var crf int
	var vmaf float64
	search := NewCrfSearch(e.logger, e.sourcePath, e.targetVMAF, e.transcodeConfig)
	if crf, vmaf, err = search.Run(ctx); err != nil {
		e.logger.Fatal().Err(err).Msg("failed to run crf search")
	}
	e.logger.Info().Msgf("Done: Found crf: %d, vmaf: %.2f", crf, vmaf)

	e.transcodeConfig.VideoCRF = crf
	if err = commands.NewFfmpegEncode(e.logger, e.sourcePath, e.targetPath, e.transcodeConfig).Run(ctx); err != nil {
		return err
	}

	if vmaf, err = commands.NewFfmpegVMAF(e.logger, e.targetPath, e.sourcePath, 5).Run(ctx); err != nil {
		return err
	}
	e.logger.Info().Msgf("Done: Optimized encode with crf: %d, score: %.2f", crf, vmaf)

	return nil
}
