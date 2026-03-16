package actions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/model"
)

type OptimizedEncoded struct {
	logger          *slog.Logger
	transcodeConfig commands.TranscodeConfig
	targetVMAF      float64
	crfInitial      int
	crfMin          int
	crfMax          int
	tolerance       float64
	sourcePath      string
	targetPath      string
}

func NewOptimizedEncoded(logger *slog.Logger, cfg commands.TranscodeConfig, source string, target string, targetVMAF float64, crfInitial int, crfMin int, crfMax int, tolerance float64) *OptimizedEncoded {
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
	var err error
	var crf int
	var vmaf float64
	search := NewCrfSearch(e.logger, e.sourcePath, e.targetVMAF, e.crfInitial, e.crfMin, e.crfMax, e.tolerance, e.transcodeConfig)
	if crf, vmaf, err = search.Run(ctx); err != nil {
		return fmt.Errorf("crf search failed: %w", err)
	}
	e.logger.Info("crf search complete", "crf", crf, "vmaf", vmaf)

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

	tracks := metadata.GetVideoTracks()
	if len(tracks) == 0 {
		return fmt.Errorf("no video tracks in encoded output")
	}

	averageBitrateKBPS := int(tracks[0].BitRate) / 1000 //nolint:mnd
	streamSizeMB := int(tracks[0].StreamSize) / 1000000 //nolint:mnd

	e.logger.Info("optimized encode complete",
		"crf", crf,
		"vmaf", fmt.Sprintf("%.2f", vmaf),
		"avgBitrate", fmt.Sprintf("%dKbps", averageBitrateKBPS),
		"streamSize", fmt.Sprintf("%dMB", streamSizeMB),
	)

	return nil
}
