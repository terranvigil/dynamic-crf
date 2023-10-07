package main

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/actions"
	"github.com/terranvigil/dynamic-crf/commands"
	//"golang.org/x/text/language"
	//"golang.org/x/text/message"
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
	var err error

	cfg := commands.TranscodeConfig{
		VideoCodec: "libx264",
		// 1080p = 1920x1080
		Width:               1920,
		VideoMaxBitrateKbps: 12000,
		VideoBufferSizeKbps: 48000,
		Tune:                "animation",
		// TODO suggest a starting CRF for the search and then use the result for the
		//   search range, either as max or min depending on the result
		// VideoCRF:   27,
	}

	if err = actions.NewOptimizedEncoded(logger, cfg, 93, sourcePath, "optimized.mp4").Run(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to run optimized encode")
	}

	os.Exit(0)
}

func setup() (logger zerolog.Logger) {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	logger = zerolog.New(os.Stderr).With().Timestamp().Logger()

	logger = logger.Level(zerolog.InfoLevel)

	return logger
}
