package actions

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/terranvigil/dynamic-crf/commands"
)

// OptimizedEncode searches for a CRF, encodes the full source at it and
// scores the result on every frame.
type OptimizedEncode struct {
	logger          *slog.Logger
	transcodeConfig commands.TranscodeConfig
	params          SearchParams
	vmafOpts        commands.VMAFOptions
	sourcePath      string
	targetPath      string
	// overrides for tests
	search func(ctx context.Context) (SearchResult, error)
	encode func(ctx context.Context, crf float64) (Score, error)
}

// OptimizeResult is the search result plus the full encode's own score.
type OptimizeResult struct {
	Search      SearchResult `json:"search"`
	CRF         float64      `json:"crf"`
	VMAF        float64      `json:"vmaf"`
	BitrateKbps int          `json:"bitrateKbps"`
	Model       string       `json:"model"`
	Output      string       `json:"output"`
	// Corrected is true when the full encode missed the target and was redone
	// one CRF step higher in quality.
	Corrected bool `json:"corrected"`
	// MeetsTarget is whether the full encode's own score reached the target.
	MeetsTarget bool `json:"meetsTarget"`
}

func NewOptimizedEncode(logger *slog.Logger, cfg commands.TranscodeConfig, source, target string, params SearchParams, vmafOpts commands.VMAFOptions) *OptimizedEncode {
	return &OptimizedEncode{
		logger:          logger,
		transcodeConfig: cfg,
		params:          params,
		vmafOpts:        vmafOpts,
		sourcePath:      source,
		targetPath:      target,
	}
}

func (e *OptimizedEncode) Run(ctx context.Context) (OptimizeResult, error) {
	search := e.search
	if search == nil {
		search = NewCrfSearch(e.logger, e.sourcePath, e.params, e.transcodeConfig, e.vmafOpts).Run
	}
	encode := e.encode
	if encode == nil {
		encode = e.encodeFull
	}

	if e.search == nil {
		if err := commands.CheckAudioMux(ctx, e.logger, e.sourcePath, e.transcodeConfig); err != nil {
			return OptimizeResult{}, err
		}
	}
	sr, err := search(ctx)
	if err != nil {
		return OptimizeResult{}, fmt.Errorf("crf search failed: %w", err)
	}

	crf := sr.CRF
	if sr.CambiHintDropped {
		e.vmafOpts.SkipCambiHint = true
	}
	if sr.ChromaDropped {
		e.vmafOpts.QuietChromaWarning = true
	}
	full, err := encode(ctx, crf)
	if err != nil {
		return OptimizeResult{}, err
	}
	res := OptimizeResult{Search: sr, CRF: crf, VMAF: full.VMAF, BitrateKbps: full.BitrateKbps, Model: full.Model, Output: e.targetPath}

	// a scene sample can flatter the full title. under the floor policy, one
	// step toward quality usually recovers a small miss.
	next := math.Round((crf-e.params.CRFStep)*1e6) / 1e6
	if e.params.Policy == PolicyFloor && full.VMAF < e.params.TargetVMAF && next >= e.params.CRFLow &&
		sr.Status != StatusUnreachable && sr.Status != StatusPlateau {
		e.logger.Warn("full encode missed target, re-encoding one step toward quality",
			"crf", commands.FormatCRF(crf), "vmaf", fmt.Sprintf("%.2f", full.VMAF), "next", commands.FormatCRF(next))
		if full, err = encode(ctx, next); err != nil {
			return OptimizeResult{}, err
		}
		res.CRF, res.VMAF, res.BitrateKbps, res.Corrected = next, full.VMAF, full.BitrateKbps, true
	}

	res.MeetsTarget = res.VMAF >= e.params.TargetVMAF
	if !res.MeetsTarget {
		e.logger.Warn("full encode is below the target", "vmaf", fmt.Sprintf("%.2f", res.VMAF), "target", e.params.TargetVMAF)
	}
	e.logger.Info("optimized encode complete",
		"crf", commands.FormatCRF(res.CRF),
		"vmaf", fmt.Sprintf("%.2f", res.VMAF),
		"model", res.Model,
		"avgBitrate", fmt.Sprintf("%dKbps", res.BitrateKbps),
	)
	return res, nil
}

func (e *OptimizedEncode) encodeFull(ctx context.Context, crf float64) (Score, error) {
	cfg := e.transcodeConfig
	cfg.VideoCRF = crf
	if err := commands.NewFfmpegEncode(e.logger, e.sourcePath, e.targetPath, cfg).Run(ctx); err != nil {
		return Score{}, err
	}
	opts := e.vmafOpts.WithSubsample(1)
	opts.StrictFrames = true
	res, err := commands.NewVMAF(e.logger, e.targetPath, e.sourcePath, opts).Run(ctx)
	if err != nil {
		return Score{}, fmt.Errorf("score full encode: %w", err)
	}
	if res.CambiHintDropped {
		e.vmafOpts.SkipCambiHint = true
	}
	if res.ChromaDropped {
		e.vmafOpts.QuietChromaWarning = true
	}
	return Score{VMAF: res.Score, BitrateKbps: res.Distorted.BitrateKbps(), MaxScore: res.MaxScore, Model: res.Model}, nil
}
