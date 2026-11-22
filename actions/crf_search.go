package actions

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/inspect"
)

const (
	defaultSceneSampleMinDurSec = 60
	// DefaultCoarseSubsample scores every 5th frame while searching. On v1
	// that moves scores by about 0.2 on short clips, which the exact re-score
	// of the winner and its near-miss neighbor absorbs.
	DefaultCoarseSubsample = 5
)

// CrfSearch finds the CRF that meets a target VMAF. Sources of a minute or
// more are scored on a sample of their sharpest scene cuts rather than in full.
type CrfSearch struct {
	logger          *slog.Logger
	sourcePath      string
	params          SearchParams
	transcodeConfig commands.TranscodeConfig
	vmafOpts        commands.VMAFOptions
	// scoreFn overrides encoding and scoring, for tests
	scoreFn ScoreFunc
	// sceneSampleMinDurSec is the source duration at which scene sampling
	// starts. Zero means 60s.
	sceneSampleMinDurSec float64
}

func NewCrfSearch(logger *slog.Logger, source string, params SearchParams, transcodeConfig commands.TranscodeConfig, vmafOpts commands.VMAFOptions) *CrfSearch {
	return &CrfSearch{
		logger:          logger,
		sourcePath:      source,
		params:          params,
		transcodeConfig: transcodeConfig,
		vmafOpts:        vmafOpts,
	}
}

func (c *CrfSearch) Run(ctx context.Context) (SearchResult, error) {
	c.logger.Info("running dynamic-crf search", "target", c.params.TargetVMAF, "policy", c.params.Policy,
		"crfLow", c.params.CRFLow, "crfHigh", c.params.CRFHigh, "crfStep", c.params.CRFStep)

	if err := c.params.Validate(); err != nil {
		return SearchResult{}, err
	}

	score := c.scoreFn
	if score == nil {
		reference, model, cleanup, err := c.prepare(ctx)
		if err != nil {
			return SearchResult{}, err
		}
		defer cleanup()

		// pin the model the source resolved to: a scene sample can probe to a
		// different rate than its source and would otherwise pick another
		opts := c.vmafOpts
		opts.Model = model
		scorer, err := NewEncodeScorer(c.logger, c.transcodeConfig, reference, opts)
		if err != nil {
			return SearchResult{}, err
		}
		defer scorer.Close() //nolint:errcheck
		// decide the coarse stride once, so a clip short enough to score every
		// frame doesn't get a second identical "exact" pass
		if c.params.CoarseSubsample, err = scorer.EffectiveSubsample(ctx, c.params.CoarseSubsample); err != nil {
			return SearchResult{}, err
		}
		score = scorer.Score
	}

	res, err := newGridSearch(c.logger, c.params, score).run(ctx)
	if err != nil {
		return SearchResult{}, err
	}
	c.logger.Info("search result", "crf", commands.FormatCRF(res.CRF), "vmaf", fmt.Sprintf("%.2f", res.VMAF),
		"status", res.Status, "scores", res.Scores, "encodes", res.Encodes)
	return res, nil
}

// prepare checks the model and vmaf binary against the source before any
// encoding, then returns the clip to search on (the source itself, or a scene
// sample for long sources) and the model the source resolved to. cleanup
// removes any sample.
func (c *CrfSearch) prepare(ctx context.Context) (path, model string, cleanup func(), err error) {
	noop := func() {}
	info, err := commands.ProbeVideo(ctx, c.logger, c.sourcePath)
	if err != nil {
		return "", "", noop, fmt.Errorf("probe source: %w", err)
	}

	model = commands.SelectModel(c.vmafOpts.Model, info.Width, info.Height, info.FPS)
	if maxScore := commands.ModelMaxScore(model); c.params.TargetVMAF >= maxScore {
		return "", "", noop, fmt.Errorf("target vmaf %.2f must be below model %s's maximum of %g",
			c.params.TargetVMAF, commands.ModelName(model), maxScore)
	}
	if err = commands.CheckModelSize(model, info.Width, info.Height); err != nil {
		return "", "", noop, err
	}
	check := c.vmafOpts
	check.Model = model
	if err = commands.CheckVMAF(ctx, c.logger, check); err != nil {
		return "", "", noop, fmt.Errorf("vmaf check failed before searching: %w", err)
	}

	minDur := c.sceneSampleMinDurSec
	if minDur == 0 {
		minDur = defaultSceneSampleMinDurSec
	}
	if info.VideoDuration() < minDur {
		c.logger.Info("source is shorter than the sampling threshold, searching on the full source",
			"duration", fmt.Sprintf("%.1fs", info.VideoDuration()), "threshold", fmt.Sprintf("%.0fs", minDur))
		return c.sourcePath, model, noop, nil
	}

	sample, err := c.createSceneSample(ctx)
	if err != nil {
		return "", "", noop, fmt.Errorf("failed to create scene sample: %w", err)
	}
	return sample, model, func() { os.Remove(sample) }, nil //nolint:errcheck
}

func (c *CrfSearch) createSceneSample(ctx context.Context) (string, error) {
	f, err := os.CreateTemp("", "sample_*"+commands.SampleExt)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for sample: %w", err)
	}
	path := f.Name()
	_ = f.Close()

	scenes, _, err := inspect.DetectScenes(c.logger, ctx, c.sourcePath)
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("failed to detect scenes in source: %w", err)
	}
	if err = commands.NewFfmpegSampler(c.logger, c.sourcePath, path, scenes).Run(ctx); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("failed to sample source: %w", err)
	}
	return path, nil
}
