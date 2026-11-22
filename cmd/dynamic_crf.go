package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"syscall"

	"github.com/terranvigil/dynamic-crf/actions"
	"github.com/terranvigil/dynamic-crf/commands"
)

// encoders known to take fractional crf
var fractionalCRFCodecs = map[string]bool{"libx264": true, "libx265": true}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	o, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	if err = o.validate(); err != nil {
		return err
	}

	level := slog.LevelInfo
	if o.verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	for _, d := range o.deprecated {
		logger.Warn("deprecated flag", "note", d)
	}
	if o.search.CRFStep != math.Trunc(o.search.CRFStep) && !fractionalCRFCodecs[o.codec] {
		logger.Warn("this encoder may round fractional crf, so neighboring grid points can encode identically",
			"codec", o.codec, "crfstep", o.search.CRFStep)
	}

	// cancellable context for graceful shutdown on signal
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	result, err := dispatch(ctx, logger, o)
	if err != nil {
		return err
	}
	if o.jsonOut && result != nil {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	return nil
}

// scoreResult is the JSON shape for the encode and vmaf actions.
type scoreResult struct {
	VMAF        float64 `json:"vmaf"`
	Model       string  `json:"model"`
	Frames      int     `json:"frames"`
	BitrateKbps int     `json:"bitrateKbps"`
	SizeBytes   int64   `json:"sizeBytes"`
	CRF         float64 `json:"crf,omitempty"`
	Output      string  `json:"output,omitempty"`
}

func newScoreResult(r *commands.VMAFResult) scoreResult {
	return scoreResult{
		VMAF:        r.Score,
		Model:       r.Model,
		Frames:      r.Frames,
		BitrateKbps: r.Distorted.BitrateKbps(),
		SizeBytes:   r.Distorted.SizeBytes,
	}
}

func dispatch(ctx context.Context, logger *slog.Logger, o *options) (any, error) {
	cfg := o.transcodeConfig()
	exact := o.vmaf.WithSubsample(1)

	switch o.action {
	case actionOptimize:
		return actions.NewOptimizedEncode(logger, cfg, o.sourcePath, o.targetPath, o.search, o.vmaf).Run(ctx)

	case actionSearch:
		res, err := actions.NewCrfSearch(logger, o.sourcePath, o.search, cfg, o.vmaf).Run(ctx)
		if err != nil {
			return nil, err
		}
		logger.Info("search complete", "crf", commands.FormatCRF(res.CRF), "vmaf", fmt.Sprintf("%.2f", res.VMAF), "status", res.Status)
		return res, nil

	case actionEncode:
		// refuse an unscorable source before writing -o
		info, err := commands.ProbeVideo(ctx, logger, o.sourcePath)
		if err != nil {
			return nil, fmt.Errorf("probe source: %w", err)
		}
		model := commands.SelectModel(o.vmaf.Model, info.Width, info.Height, info.FPS)
		if err = commands.CheckModelSize(model, info.Width, info.Height); err != nil {
			return nil, err
		}
		check := o.vmaf
		check.Model = model
		if err = commands.CheckVMAF(ctx, logger, check); err != nil {
			return nil, fmt.Errorf("vmaf check failed before encoding: %w", err)
		}
		if err := commands.NewFfmpegEncode(logger, o.sourcePath, o.targetPath, cfg).Run(ctx); err != nil {
			return nil, err
		}
		score, err := commands.NewVMAF(logger, o.targetPath, o.sourcePath, exact).Run(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to calc vmaf of output: %w", err)
		}
		res := newScoreResult(score)
		res.CRF, res.Output = o.crf, o.targetPath
		logger.Info("encode complete", "vmaf", fmt.Sprintf("%.2f", res.VMAF), "model", res.Model, "avgBitrate", fmt.Sprintf("%dKbps", res.BitrateKbps))
		return res, nil

	case actionInspect:
		metadata, err := commands.NewMediaInfo(logger, o.sourcePath).Run(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to run mediainfo: %w", err)
		}
		inspectPath := o.sourcePath + "_inspect.json"
		data, err := json.MarshalIndent(metadata, "", " ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal json: %w", err)
		}
		if err = os.WriteFile(inspectPath, data, 0o600); err != nil {
			return nil, fmt.Errorf("failed to write json: %w", err)
		}
		logger.Info("inspect complete", "output", inspectPath)
		return map[string]string{"output": inspectPath}, nil

	case actionVmaf:
		score, err := commands.NewVMAF(logger, o.targetPath, o.sourcePath, exact).Run(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to calc vmaf: %w", err)
		}
		res := newScoreResult(score)
		logger.Info("vmaf complete", "vmaf", fmt.Sprintf("%.2f", res.VMAF), "model", res.Model, "frames", res.Frames,
			"avgBitrate", fmt.Sprintf("%dKbps", res.BitrateKbps))
		return res, nil

	case actionCambi:
		res, err := commands.NewCambi(logger, o.targetPath, o.vmaf).Run(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to calc cambi: %w", err)
		}
		logger.Info("cambi complete", "max", fmt.Sprintf("%.2f", res.Max), "mean", fmt.Sprintf("%.2f", res.Mean))
		return map[string]float64{"max": res.Max, "mean": res.Mean}, nil
	}
	return nil, fmt.Errorf("unhandled action %q", o.action)
}
