package main

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/actions"
	"github.com/terranvigil/dynamic-crf/commands"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// TODO:
// -- add vmaf models to project
// -- select vmaf model based on resolution

// vmaf of 93 is generally considered "good enough"
// nobody has produced a trained vmaf model for anime
// we should train our own models, SD and HD resolutions (phone and TV)
// a CRF of 23 is also generally considered "good enough"
// we probably want something closer to 21 for anime, capping using VBV/HRD

func main() {
	logger := setup()
	sourcePath := "/Users/terran.vigil/media/perseverance_1280.mv4"
	// TODO need a better way to cancel this if not progressing
	ctx := context.Background()

	// search for optimized crf using a target vmaf
	/*
	var err error
	var crf int
	var vmaf float64
	search := actions.NewCrfSearch(logger, sourcePath, 93)
	if crf, vmaf, err = search.Run(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to run crf search")
	}
	logger.Info().Msgf("Done: Found crf: %d, vmaf: %f", crf, vmaf)
	*/

	// get vmaf for optimized crf
	/*
	cfg := &commands.TranscodeConfig{
		VideoCodec: "libx264",
		VideoCRF:   27,
		//VideoBitrateKbps: 4000,
		VideoMaxBitrateKbps: 8000,
		VideoBufferSizeKbps: 12000,
	}
	score, bitrate, size, err := actions.NewVMAFScore(logger, cfg, sourcePath).Run(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to run vmaf score")
	}
	streamSizeKB := message.NewPrinter(language.English).Sprintf("%d", size/1000)
	logger.Info().Msgf("Done: Found score: %f, bitrate: %d, size: %sMB", score, bitrate, streamSizeKB)
	*/

	// get vmaf for legacy abr encode
	cfg := &commands.TranscodeConfig{
		VideoCodec:       "libx264",
		VideoBitrateKbps: 4000,
		VideoBufferSizeKbps: 9000,
		VideoMinBitrateKbps: 4000,
		VideoMaxBitrateKbps: 6000,
	}
	score, bitrate, size, err := actions.NewVMAFScore(logger, cfg, sourcePath).Run(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to run vmaf score")
	}
	streamSizeKB := message.NewPrinter(language.English).Sprintf("%d", size/1000)
	logger.Info().Msgf("Done: Found score: %f, bitrate: %d, size: %sMB", score, bitrate, streamSizeKB)

	os.Exit(0)
}

func setup() (logger zerolog.Logger) {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	logger = zerolog.New(os.Stderr).With().Timestamp().Logger()

	logger = logger.Level(zerolog.InfoLevel)

	return logger
}
