package actions

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/terranvigil/dynamic-crf/commands"
)

// EncodeScorer encodes a reference at a given CRF and scores it with VMAF.
// Encodes are kept until Close, so re-scoring a CRF on every frame after a
// coarse pass doesn't encode it again.
type EncodeScorer struct {
	logger        *slog.Logger
	cfg           commands.TranscodeConfig
	referencePath string
	vmafOpts      commands.VMAFOptions
	dir           string
	encodes       map[float64]string
	refInfo       *commands.VideoInfo
}

func NewEncodeScorer(logger *slog.Logger, cfg commands.TranscodeConfig, referencePath string, vmafOpts commands.VMAFOptions) (*EncodeScorer, error) {
	dir, err := os.MkdirTemp("", "dcrf_encodes_*")
	if err != nil {
		return nil, fmt.Errorf("create encode temp dir: %w", err)
	}
	// search encodes are only scored, so audio would just cost time
	cfg.AudioCodec = commands.AudioNone
	vmafOpts.StrictFrames = true
	return &EncodeScorer{
		logger:        logger,
		cfg:           cfg,
		referencePath: referencePath,
		vmafOpts:      vmafOpts,
		dir:           dir,
		encodes:       map[float64]string{},
	}, nil
}

// Close removes every encode made by this scorer.
func (s *EncodeScorer) Close() error {
	return os.RemoveAll(s.dir)
}

// minCoarseFrames keeps a coarse pass from scoring so few frames that it says
// nothing about the clip
const minCoarseFrames = 20

// clampSubsample caps the stride so a coarse pass still scores at least
// minCoarseFrames frames of the reference.
func (s *EncodeScorer) clampSubsample(sub int) int {
	frames := s.refInfo.VideoFrames
	if frames <= 0 {
		frames = int(s.refInfo.Duration * s.refInfo.FPS)
	}
	if frames <= 0 {
		return sub
	}
	return min(sub, max(1, frames/minCoarseFrames))
}

// EffectiveSubsample probes the reference and returns the coarse stride the
// scorer will actually use for sub.
func (s *EncodeScorer) EffectiveSubsample(ctx context.Context, sub int) (int, error) {
	if err := s.probe(ctx); err != nil {
		return 0, err
	}
	return s.clampSubsample(max(1, sub)), nil
}

func (s *EncodeScorer) probe(ctx context.Context) error {
	if s.refInfo != nil {
		return nil
	}
	info, err := commands.ProbeVideo(ctx, s.logger, s.referencePath)
	if err != nil {
		return fmt.Errorf("probe reference: %w", err)
	}
	s.refInfo = info
	return nil
}

// Score satisfies ScoreFunc.
func (s *EncodeScorer) Score(ctx context.Context, crf float64, subsample int) (Score, error) {
	if err := s.probe(ctx); err != nil {
		return Score{}, err
	}

	path, ok := s.encodes[crf]
	if !ok {
		path = filepath.Join(s.dir, "crf_"+commands.FormatCRF(crf)+".mp4")
		cfg := s.cfg
		cfg.VideoCRF = crf
		if err := commands.NewFfmpegEncode(s.logger, s.referencePath, path, cfg).Run(ctx); err != nil {
			return Score{}, fmt.Errorf("encode at crf %s: %w", commands.FormatCRF(crf), err)
		}
		s.encodes[crf] = path
	}

	res, err := commands.NewVMAF(s.logger, path, s.referencePath, s.vmafOpts.WithSubsample(s.clampSubsample(subsample))).
		WithReferenceInfo(s.refInfo).Run(ctx)
	if err != nil {
		return Score{}, fmt.Errorf("vmaf at crf %s: %w", commands.FormatCRF(crf), err)
	}
	if res.CambiHintDropped {
		s.vmafOpts.SkipCambiHint = true
	}
	if res.ChromaDropped {
		s.vmafOpts.QuietChromaWarning = true
	}
	return Score{
		VMAF:        res.Score,
		BitrateKbps: res.Distorted.BitrateKbps(),
		MaxScore:    res.MaxScore,
		Model:       res.Model,

		CambiHintDropped: res.CambiHintDropped,
		ChromaDropped:    res.ChromaDropped,
	}, nil
}
