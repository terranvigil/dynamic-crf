package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/terranvigil/dynamic-crf/actions"
	"github.com/terranvigil/dynamic-crf/commands"
)

const (
	actionOptimize = "optimize"
	actionSearch   = "search"
	actionEncode   = "encode"
	actionInspect  = "inspect"
	actionVmaf     = "vmaf"
	actionCambi    = "cambi"
)

var validActions = []string{
	actionOptimize,
	actionSearch,
	actionEncode,
	actionInspect,
	actionVmaf,
	actionCambi,
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
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

	flag.StringVar(&action, "action", "", "action to perform: optimize, search, encode, inspect, vmaf, cambi")
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
	flag.IntVar(&minBitrateKbps, "minbitrate", 0, "limit minimum bitrate of output video")
	flag.StringVar(&tune, "tune", "", "tune flag for encoder: animation, film, grain, psnr, ssim")
	flag.StringVar(&tune, "t", "", "tune flag for encoder: animation, film, grain, psnr, ssim")

	flag.Parse()

	if err := validateFlags(action, crf, bitrateKbps, sourcePath, targetPath, tune,
		targetVMAF, searchTolerance, minCRF, maxCRF, initCRF); err != nil {
		flag.Usage()
		return err
	}

	if strings.HasPrefix(sourcePath, "~/") {
		dirname, _ := os.UserHomeDir()
		sourcePath = filepath.Join(dirname, sourcePath[2:])
	}
	if strings.HasPrefix(targetPath, "~/") {
		dirname, _ := os.UserHomeDir()
		targetPath = filepath.Join(dirname, targetPath[2:])
	}

	// cancellable context for graceful shutdown on signal
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

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
		return actions.NewOptimizedEncoded(logger, cfg, sourcePath, targetPath, targetVMAF, initCRF, minCRF, maxCRF, searchTolerance).Run(ctx)
	case actionSearch:
		selectedCRF, vmaf, err := actions.NewCrfSearch(logger, sourcePath, targetVMAF, initCRF, minCRF, maxCRF, searchTolerance, cfg).Run(ctx)
		if err != nil {
			return err
		}
		logger.Info("search complete", "crf", selectedCRF, "vmaf", fmt.Sprintf("%.2f", vmaf))
	case actionEncode:
		if err := commands.NewFfmpegEncode(logger, sourcePath, targetPath, cfg).Run(ctx); err != nil {
			return err
		}
		score, err := commands.NewFfmpegVMAF(logger, sourcePath, targetPath, defaultVMAFSpeed).Run(ctx)
		if err != nil {
			return fmt.Errorf("failed to calc vmaf of output: %w", err)
		}
		var encodeType string
		if crf > 0 {
			encodeType = fmt.Sprintf("crf: %d", crf)
		} else {
			encodeType = fmt.Sprintf("bitrate: %d", bitrateKbps)
		}
		logger.Info("encode complete", "type", encodeType, "vmaf", fmt.Sprintf("%.2f", score))
	case actionInspect:
		metadata, err := commands.NewMediaInfo(logger, sourcePath).Run(ctx)
		if err != nil {
			return fmt.Errorf("failed to run mediainfo: %w", err)
		}
		inspectPath := sourcePath + "_inspect.json"
		data, err := json.MarshalIndent(metadata, "", " ")
		if err != nil {
			return fmt.Errorf("failed to marshal json: %w", err)
		}
		if err = os.WriteFile(inspectPath, data, 0o600); err != nil {
			return fmt.Errorf("failed to write json: %w", err)
		}
		logger.Info("inspect complete", "output", inspectPath)
	case actionVmaf:
		referencePath := sourcePath
		distortedPath := targetPath
		score, err := commands.NewFfmpegVMAF(logger, distortedPath, referencePath, defaultVMAFSpeed).Run(ctx)
		if err != nil {
			return fmt.Errorf("failed to calc vmaf: %w", err)
		}
		metadata, err := commands.NewMediaInfo(logger, distortedPath).Run(ctx)
		if err != nil {
			return fmt.Errorf("failed to get mediainfo of output: %w", err)
		}
		tracks := metadata.GetVideoTracks()
		if len(tracks) == 0 {
			return fmt.Errorf("no video tracks in distorted file")
		}
		averageBitrateKBPS := int(tracks[0].BitRate) / 1000    //nolint:mnd
		maxBitrateKBPS := int(tracks[0].BitRateMaximum) / 1000 //nolint:mnd
		streamSizeKB := int(tracks[0].StreamSize) / 1000       //nolint:mnd
		logger.Info("vmaf complete",
			"vmaf", fmt.Sprintf("%.2f", score),
			"avgBitrate", fmt.Sprintf("%dKbps", averageBitrateKBPS),
			"maxBitrate", fmt.Sprintf("%dKbps", maxBitrateKBPS),
			"streamSize", fmt.Sprintf("%dKB", streamSizeKB),
		)
	case actionCambi:
		referencePath := sourcePath
		distortedPath := targetPath
		max, mean, err := commands.NewCambi(logger, distortedPath, referencePath).Run(ctx)
		if err != nil {
			return fmt.Errorf("failed to calc cambi: %w", err)
		}
		logger.Info("cambi complete", "max", fmt.Sprintf("%.2f", max), "mean", fmt.Sprintf("%.2f", mean))
	}

	return nil
}

func validateFlags(action string, crf, bitrateKbps int, sourcePath, targetPath, tune string,
	targetVMAF, tolerance float64, minCRF, maxCRF, initCRF int) error {
	if !slices.Contains(validActions, action) {
		return fmt.Errorf("invalid action: %q, must be one of: %s", action, strings.Join(validActions, ", "))
	}
	if action == actionEncode {
		if bitrateKbps <= 0 && crf <= 0 {
			return fmt.Errorf("bitrate or crf required for encode action")
		}
	} else if crf > 0 || bitrateKbps > 0 {
		return fmt.Errorf("bitrate and crf flags are only valid for encode action")
	}
	if sourcePath == "" {
		return fmt.Errorf("source path (-i) is required")
	}
	if action == actionSearch || action == actionInspect {
		if targetPath != "" {
			return fmt.Errorf("target path not allowed for %s action", action)
		}
	} else if targetPath == "" || !strings.HasSuffix(targetPath, ".mp4") {
		return fmt.Errorf("target path (-o) of {name}.mp4 is required")
	}
	validTunes := []string{"", "animation", "film", "grain", "psnr", "ssim"}
	if !slices.Contains(validTunes, tune) {
		return fmt.Errorf("invalid tune value: %q", tune)
	}
	if targetVMAF <= 0 || targetVMAF > 100 {
		return fmt.Errorf("target vmaf (%.2f) must be between 0 and 100", targetVMAF)
	}
	if tolerance <= 0 {
		return fmt.Errorf("tolerance (%.2f) must be positive", tolerance)
	}
	if minCRF <= maxCRF {
		return fmt.Errorf("min crf (%d) must be greater than max crf (%d)", minCRF, maxCRF)
	}
	if initCRF < maxCRF || initCRF > minCRF {
		return fmt.Errorf("initial crf (%d) must be between max (%d) and min (%d)", initCRF, maxCRF, minCRF)
	}
	return nil
}
