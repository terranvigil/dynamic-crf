package main

import (
	"context"
	"flag"
	"os"
	"strings"
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
	zerolog.TimeFieldFormat = time.RFC3339Nano
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	logger = logger.Level(zerolog.InfoLevel)

	var (
		action                 string
		sourcePath, targetPath string
		targetVMAF             float64
		codec                  string
		width, height          int
		maxBitrateKbps         int
		bufferSizeKbps         int
		tune                   string
	)

	flag.StringVar(&action, "action", "", "action to perform: optimize, search, encode")
	flag.StringVar(&action, "a", "", "action to perform: optimize, encode")
	flag.StringVar(&sourcePath, "input", "", "path to input file")
	flag.StringVar(&sourcePath, "i", "", "path to input file")
	flag.StringVar(&targetPath, "output", "", "path to output file")
	flag.StringVar(&targetPath, "o", "", "path to output file")
	flag.Float64Var(&targetVMAF, "targetvmaf", 93.0, "target vmaf score")
	flag.StringVar(&codec, "codec", "libx264", "video codec to use")
	flag.StringVar(&codec, "c", "libx264", "video codec to use")
	flag.IntVar(&width, "width", 0, "width of output video")
	flag.IntVar(&width, "w", 0, "width of output video")
	flag.IntVar(&height, "height", 0, "height of output video")
	flag.IntVar(&height, "h", 0, "height of output video")
	flag.IntVar(&maxBitrateKbps, "maxbitrate", 0, "limit peak bitrate of output video")
	flag.IntVar(&maxBitrateKbps, "mb", 0, "limit peak bitrate of output video")
	flag.IntVar(&bufferSizeKbps, "buffersize", 0, "hrd buffer size of output video")
	flag.IntVar(&bufferSizeKbps, "bs", 0, "hrd buffer size of output video")
	flag.StringVar(&tune, "tune", "", "tune flag for encoder: animation, film, grain, psnr, ssim")
	flag.StringVar(&tune, "t", "", "tune flag for encoder: animation, film, grain, psnr, ssim")

	flag.Parse()

	if action == "" || (action != "optimize" && action != "search" && action != "encode") {
		flag.Usage()
		logger.Fatal().Msg("invalid action")
	}
	if sourcePath == "" {
		flag.Usage()
		logger.Fatal().Msg("source path required")
	}
	if targetPath == "" || !strings.HasSuffix(targetPath, ".mp4") {
		flag.Usage()
		logger.Fatal().Msg("target path of {name}.mp4 required")
	}
	if tune != "" && tune != "animation" && tune != "film" && tune != "grain" && tune != "psnr" && tune != "ssim" {
		flag.Usage()
		logger.Fatal().Msg("invalid tune value")
	}

	// TODO need a better way to cancel this if not progressing
	ctx := context.Background()
	var err error

	cfg := commands.TranscodeConfig{
		VideoCodec:          codec,
		Width:               width,
		Height:              height,
		VideoMaxBitrateKbps: maxBitrateKbps,
		VideoBufferSizeKbps: bufferSizeKbps,
		Tune:                tune,
	}

	switch action {
	case "optimize":
		if err = actions.NewOptimizedEncoded(logger, cfg, targetVMAF, sourcePath, targetPath).Run(ctx); err != nil {
			logger.Fatal().Err(err).Msg("failed to run optimized encode")
		}
	case "search":
		score, bitrate, size, err := actions.NewVMAFScore(logger, cfg, sourcePath).Run(ctx)
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to run vmaf score")
		}
		logger.Info().Msgf("Done: Found score: %.2f, bitrate: %dKbps, size: %dMB", score, bitrate, size/1000)
	case "default":
		logger.Fatal().Msg("no action specified")
	}

	os.Exit(0)
}
