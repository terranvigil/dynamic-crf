package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/actions"
	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/model"
)

// TODO:
// -- add vmaf models to project
// -- select vmaf model based on resolution
const (
	actionOptimize = "optimize"
	actionSearch   = "search"
	actionEncode   = "encode"
	actionScore    = "score"
	actionInspect  = "inspect"
)

var validActions = []string{
	actionOptimize,
	actionInspect,
	actionEncode,
	actionScore,
	actionSearch,
}

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

	const (
		defaultSearchTolerance = 0.5
		defaultMaxCRF          = 15
		defaultMinCRF          = 30
		defaultInitCRF         = 20
		defaultTargetVMAF      = 95.0
		defaultVMAFSpeed       = 5
	)

	flag.StringVar(&action, "action", "", "action to perform: optimize, search, encode, score, inspect")
	flag.StringVar(&action, "a", "", "action to perform: optimize, encode")
	flag.IntVar(&crf, "crf", 0, "crf value to use for encode action")
	flag.IntVar(&bitrateKbps, "bitrate", 0, "bitrate value to use for encode action")
	flag.IntVar(&maxCRF, "maxcrf", defaultMaxCRF, "maximum crf value to search, higher value == lower quality")
	flag.IntVar(&minCRF, "mincrf", defaultMinCRF, "minimum crf value to search, higher value == lower quality")
	flag.IntVar(&initCRF, "initialcrf", defaultInitCRF, "initial crf value to search")
	flag.Float64Var(&searchTolerance, "tolerance", defaultSearchTolerance, "tolerance for search")
	flag.StringVar(&sourcePath, "input", "", "path to input file")
	flag.StringVar(&sourcePath, "i", "", "path to input file")
	flag.StringVar(&targetPath, "output", "", "path to output file or distorted file if calculating vmaf")
	flag.StringVar(&targetPath, "o", "", "path to output file or distorted file if calculating vmaf")
	flag.Float64Var(&targetVMAF, "targetvmaf", defaultTargetVMAF, "target vmaf score")
	flag.StringVar(&codec, "codec", "libx264", "video codec to use")
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

	if !slices.Contains(validActions, action) {
		flag.Usage()
		logger.Fatal().Msg("invalid action")
	}
	if action == actionEncode {
		if bitrateKbps <= 0 && crf <= 0 {
			flag.Usage()
			logger.Fatal().Msg("bitrate or crf required for encode action")
		}
	} else if crf > 0 || bitrateKbps > 0 {
		flag.Usage()
		logger.Fatal().Msg("bitrate and crf not allowed for optimize or search actions")
	}
	if sourcePath == "" {
		flag.Usage()
		logger.Fatal().Msg("source path required")
	}
	if action == actionSearch || action == actionInspect {
		if targetPath != "" {
			flag.Usage()
			logger.Fatal().Msg("target path not allowed for search and inspect actions")
		}
	} else if targetPath == "" || !strings.HasSuffix(targetPath, ".mp4") {
		flag.Usage()
		logger.Fatal().Msg("target path of {name}.mp4 required")
	}
	if tune != "" && tune != "animation" && tune != "film" && tune != "grain" && tune != "psnr" && tune != "ssim" {
		flag.Usage()
		logger.Fatal().Msg("invalid tune value")
	}

	if strings.HasPrefix(sourcePath, "~/") {
		dirname, _ := os.UserHomeDir()
		sourcePath = filepath.Join(dirname, sourcePath[2:])
	}
	if strings.HasPrefix(targetPath, "~/") {
		dirname, _ := os.UserHomeDir()
		targetPath = filepath.Join(dirname, targetPath[2:])
	}

	// TODO cancel if not progressing, can monitor ffmpeg progress
	ctx := context.Background()
	var err error

	cfg := commands.TranscodeConfig{
		VideoCodec:          codec,
		Width:               width,
		Height:              height,
		VideoCRF:            crf,
		VideoBitrateKbps:    bitrateKbps,
		VideoMinBitrateKbps: minBitrateKbps,
		VideoMaxBitrateKbps: maxBitrateKbps,
		VideoBufferSizeKbps: bufferSizeKbps,
		Tune:                tune,
	}

	switch action {
	case actionOptimize:
		if err = actions.NewOptimizedEncoded(logger, cfg, sourcePath, targetPath, targetVMAF, initCRF, minCRF, maxCRF, searchTolerance).Run(ctx); err != nil {
			logger.Fatal().Err(err).Msg("failed to run optimized encode")
		}
	case actionSearch:
		var selectedCRF int
		var vmaf float64
		if selectedCRF, vmaf, err = actions.NewCrfSearch(logger, sourcePath, targetVMAF, initCRF, minCRF, maxCRF, searchTolerance, cfg).Run(ctx); err != nil {
			logger.Fatal().Err(err).Msg("failed to run crf search")
		}
		logger.Info().Msgf("Done: Found crf: %d, score: %.2f", selectedCRF, vmaf)
	case actionEncode:
		if err = commands.NewFfmpegEncode(logger, sourcePath, targetPath, cfg).Run(ctx); err != nil {
			logger.Fatal().Err(err).Msg("failed to run encode")
		}
		var score float64
		if score, err = commands.NewFfmpegVMAF(logger, sourcePath, targetPath, actions.DefaultSpeed).Run(ctx); err != nil {
			logger.Fatal().Err(err).Msgf("failed to calc vmaf of test output, err: %v", err)
		}
		var encodeType string
		if crf > 0 {
			encodeType = fmt.Sprintf("crf: %d", crf)
		} else {
			encodeType = fmt.Sprintf("bitrate: %d", bitrateKbps)
		}
		logger.Info().Msgf("Done: Encode with %s, score: %.2f", encodeType, score)
	case actionScore:
		// for readability
		referencePath := sourcePath
		distortedPath := targetPath
		var score float64
		if score, err = commands.NewFfmpegVMAF(logger, distortedPath, referencePath, defaultVMAFSpeed).Run(ctx); err != nil {
			logger.Fatal().Err(err).Msgf("failed to calc vmaf of test output, err: %v", err)
		}
		var metadata *model.MediaInfo
		if metadata, err = commands.NewMediaInfo(logger, distortedPath).Run(ctx); err != nil {
			err = fmt.Errorf("failed to get mediainfo of test output, err: %w", err)
			logger.Fatal().Err(err).Msg("failed to get mediainfo of test output")
		}

		averageBitrateKBPS := metadata.GetVideoTracks()[0].BitRate / 1000    //nolint:mnd
		maxBitrateKBPS := metadata.GetVideoTracks()[0].BitRateMaximum / 1000 //nolint:mnd
		streamSizeKB := metadata.GetVideoTracks()[0].StreamSize / 1000       //nolint:mnd
		logger.Info().Msgf("Done: VMAF: %.2f, avg bitrate: %dKbps, max bitrate: %dkbps, stream size: %dkbps", score, averageBitrateKBPS, maxBitrateKBPS, streamSizeKB)
	case actionInspect:
		var metadata *model.MediaInfo
		if metadata, err = commands.NewMediaInfo(logger, sourcePath).Run(ctx); err != nil {
			logger.Fatal().Err(err).Msg("failed to run mediainfo metadata")
		}
		targetPath = sourcePath + "_inspect.json"
		if data, err := json.MarshalIndent(metadata, "", " "); err != nil {
			logger.Fatal().Err(err).Msg("failed to marshal json")
		} else {
			if err = os.WriteFile(targetPath, data, 0o600); err != nil {
				logger.Fatal().Err(err).Msg("failed to write json")
			}
		}
		logger.Info().Msgf("Done. Wrote metadata to: %s", targetPath)
	case "default":
		logger.Fatal().Msg("invalid action specified")
	}
}
