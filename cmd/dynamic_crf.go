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
)

// TODO:
// -- add vmaf models to project
// -- select vmaf model based on resolution

func main() {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	logger = logger.Level(zerolog.InfoLevel)

	var (
		action                         string
		maxCRF, minCRF, initCRF, crf   int
		searchTolerance                float64
		sourcePath, targetPath         string
		targetVMAF                     float64
		codec                          string
		width, height                  int
		bitrateKbps                    int
		minBitrateKbps, maxBitrateKbps int
		bufferSizeKbps                 int
		tune                           string
	)

	flag.StringVar(&action, "action", "", "action to perform: optimize, search, encode")
	flag.IntVar(&crf, "crf", 0, "crf value to use for encode action")
	flag.IntVar(&bitrateKbps, "bitrate", 0, "bitrate value to use for encode action")
	flag.IntVar(&maxCRF, "maxcrf", 15, "maximum crf value to search, higher value == lower quality")
	flag.IntVar(&minCRF, "mincrf", 30, "minimum crf value to search, higher value == lower quality")
	flag.IntVar(&initCRF, "initialcrf", 20, "initial crf value to search")
	flag.Float64Var(&searchTolerance, "tolerance", 0.5, "tolerance for search")
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
	// if set, forces CBR - that's bad
	flag.IntVar(&minBitrateKbps, "minbitrate", 0, "limit minimum bitrate of output video")
	flag.StringVar(&tune, "tune", "", "tune flag for encoder: animation, film, grain, psnr, ssim")
	flag.StringVar(&tune, "t", "", "tune flag for encoder: animation, film, grain, psnr, ssim")

	flag.Parse()

	if action == "" || (action != "optimize" && action != "search" && action != "encode") {
		flag.Usage()
		logger.Fatal().Msg("invalid action")
	}
	if action == "encode" {
		if bitrateKbps == 0 && crf == 0 {
			flag.Usage()
			logger.Fatal().Msg("bitrate or crf required for encode action")
		}
	} else if crf != 0 || bitrateKbps != 0 {
		flag.Usage()
		logger.Fatal().Msg("bitrate and crf not allowed for optimize or search actions")
	}
	if sourcePath == "" {
		flag.Usage()
		logger.Fatal().Msg("source path required")
	}
	if action == "search" {
		if targetPath != "" {
			flag.Usage()
			logger.Fatal().Msg("target path not allowed for search action")
		}
	} else if targetPath == "" || !strings.HasSuffix(targetPath, ".mp4") {
		flag.Usage()
		logger.Fatal().Msg("target path of {name}.mp4 required")
	}
	if tune != "" && tune != "animation" && tune != "film" && tune != "grain" && tune != "psnr" && tune != "ssim" {
		flag.Usage()
		logger.Fatal().Msg("invalid tune value")
	}

	// TODO cancel if not progressing, can monitor ffmpeg progress
	ctx := context.Background()
	var err error

	cfg := commands.TranscodeConfig{
		VideoCodec:          codec,
		Width:               width,
		Height:              height,
		VideoBitrateKbps:    bitrateKbps,
		VideoMinBitrateKbps: minBitrateKbps,
		VideoMaxBitrateKbps: maxBitrateKbps,
		VideoBufferSizeKbps: bufferSizeKbps,
		Tune:                tune,
	}

	switch action {
	case "optimize":
		if err = actions.NewOptimizedEncoded(logger, cfg, sourcePath, targetPath, targetVMAF, initCRF, minCRF, maxCRF, searchTolerance).Run(ctx); err != nil {
			logger.Fatal().Err(err).Msg("failed to run optimized encode")
		}
	case "search":
		var selectedCRF int
		var vmaf float64
		if selectedCRF, vmaf, err = actions.NewCrfSearch(logger, sourcePath, targetVMAF, initCRF, minCRF, maxCRF, searchTolerance, cfg).Run(ctx); err != nil {
			logger.Fatal().Err(err).Msg("failed to run crf search")
		}
		logger.Info().Msgf("Done: Found crf: %d, score: %.2f", selectedCRF, vmaf)
	case "encode":
		if err = commands.NewFfmpegEncode(logger, sourcePath, targetPath, cfg).Run(ctx); err != nil {
			logger.Fatal().Err(err).Msg("failed to run encode")
		}
	case "default":
		logger.Fatal().Msg("invalid action specified")
	}

	os.Exit(0)
}
