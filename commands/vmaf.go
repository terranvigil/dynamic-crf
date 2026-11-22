package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// ModelAuto picks a VMAF v1 model from the reference's height and frame rate.
	ModelAuto = "auto"

	modelV1Prefix    = "vmaf_v1.0.16_"
	modelV1HFRPrefix = "vmaf_v1.0.16_hfr_"
	// v1 ships separate models for content at about 50 fps and up
	hfrMinFPS = 49.9
	// v1's 4K models are for content above 1080p
	uhdMinHeight = 1081

	// pooled metric name we give the model in vmaf's output
	vmafMetricName = "vmaf"
	// v1's size limits in libvmaf 3.2.1: SpEED fails below 160 on a side, and
	// small squares fail to flush (measured near 220 on the long side, so 240
	// leaves a margin). past 7680 wide CAMBI refuses its encode-size hint,
	// which the retry drops. CAMBI's window grows with width plus height and
	// can't run past 12,092 (7692x4400 and 6046x6046 score, 12,094 fails;
	// DCI 8K doesn't fit)
	v1MinLongSide        = 240
	v1MinShortSide       = 160
	v1MaxWidthPlusHeight = 12092
	// share of scored frames with a singular chroma estimate that means the
	// whole picture is too small, not just some flat frames
	chromaDroppedShare = 0.9

	// both legs are scored at 10 bits, as Netflix recommends for v1 even for 8-bit encodes
	scorePixFmt = "yuv420p10le"
)

// VMAFOptions configures the vmaf CLI used for scoring.
type VMAFOptions struct {
	// Bin is the vmaf executable. Empty means $VMAF_BIN, then "vmaf" on PATH.
	Bin string
	// Model is a model name such as vmaf_v1.0.16_3d0h, a path to a model JSON
	// file, or ModelAuto (the default when empty).
	Model string
	// ModelDir holds model JSON files, searched one level deep. Empty means use
	// the models built into libvmaf.
	ModelDir string
	// Subsample scores every Nth frame. Values below 2 score every frame.
	Subsample int
	// Threads for vmaf. Zero means all cores but one.
	Threads int
	// StrictFrames fails a score whose two inputs differ in length, instead of
	// warning. Encodes made from the reference should always match it.
	StrictFrames bool
	// SkipCambiHint leaves out CAMBI's encode-size hint, once libvmaf has
	// refused it for this rung
	SkipCambiHint bool
	// QuietChromaWarning skips the dropped-chroma warning, once a caller has
	// shown it
	QuietChromaWarning bool
}

// WithSubsample returns a copy of the options scoring every nth frame.
func (o VMAFOptions) WithSubsample(n int) VMAFOptions {
	o.Subsample = n
	return o
}

func (o VMAFOptions) bin() string {
	if o.Bin != "" {
		return o.Bin
	}
	if b := os.Getenv("VMAF_BIN"); b != "" {
		return b
	}
	return "vmaf"
}

func (o VMAFOptions) threads() int {
	if o.Threads > 0 {
		return o.Threads
	}
	return max(1, runtime.NumCPU()-1)
}

// SelectModel resolves ModelAuto to a v1 model: 1d5h_2160 above 1080p and
// 3d0h at or below it, each with its _hfr twin at about 50 fps and up. The
// shorter side decides, so portrait video picks the same model as landscape.
// Anything else is returned unchanged.
func SelectModel(model string, width, height int, fps float64) string {
	if model != "" && model != ModelAuto {
		return model
	}
	variant := "3d0h"
	if min(width, height) >= uhdMinHeight {
		variant = "1d5h_2160"
	}
	if fps >= hfrMinFPS {
		return modelV1HFRPrefix + variant
	}
	return modelV1Prefix + variant
}

// ModelName strips any directory and .json suffix from a model reference.
func ModelName(model string) string {
	return strings.TrimSuffix(filepath.Base(model), ".json")
}

// ModelMaxScore is the top of a model's score range. v1's 4K TV model
// (3d0h_2160) clips at 110; every other model clips at 100.
func ModelMaxScore(model string) float64 {
	if strings.HasSuffix(ModelName(model), "_3d0h_2160") {
		return 110 //nolint:mnd
	}
	return 100 //nolint:mnd
}

func isV1Model(model string) bool {
	return strings.HasPrefix(ModelName(model), "vmaf_v1")
}

// modelParam builds the vmaf --model value's source: path= for a file, or for
// a name found under dir, and version= for a model built into libvmaf.
func modelParam(model, dir string) (string, error) {
	// vmaf splits --model on ':' with no escaping
	if strings.ContainsRune(dir, ':') {
		return "", fmt.Errorf("vmaf model dir %q can't contain ':'", dir)
	}
	if strings.ContainsRune(model, ':') {
		return "", fmt.Errorf("vmaf model %q can't contain ':'", model)
	}
	if strings.HasSuffix(model, ".json") || strings.ContainsRune(model, filepath.Separator) {
		if _, err := os.Stat(model); err != nil {
			return "", fmt.Errorf("vmaf model file: %w", err)
		}
		return "path=" + model, nil
	}
	if dir == "" {
		return "version=" + model, nil
	}
	candidates := []string{filepath.Join(dir, model+".json")}
	if nested, _ := filepath.Glob(filepath.Join(dir, "*", model+".json")); len(nested) > 0 {
		candidates = append(candidates, nested...)
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return "path=" + c, nil
		}
	}
	return "", fmt.Errorf("vmaf model %s not found in %s", model, dir)
}

// VMAF scores a distorted video against its reference with the vmaf CLI.
type VMAF struct {
	logger        *slog.Logger
	referencePath string
	distortedPath string
	opts          VMAFOptions
	refInfo       *VideoInfo
}

// VMAFResult is a pooled VMAF score and what produced it.
type VMAFResult struct {
	Score float64
	// Model is the resolved model name, e.g. vmaf_v1.0.16_3d0h
	Model     string
	MaxScore  float64
	Frames    int
	Subsample int
	Distorted *VideoInfo
	// CambiHintDropped is set when libvmaf refused the encode size and the
	// score was taken without it; later scores of the same rung can skip it
	CambiHintDropped bool
	// ChromaDropped is set when v1's chroma feature couldn't be estimated on
	// nearly every frame (picture too small for the model, or no chroma)
	ChromaDropped bool
}

func NewVMAF(logger *slog.Logger, distorted, reference string, opts VMAFOptions) *VMAF {
	return &VMAF{
		logger:        logger,
		distortedPath: distorted,
		referencePath: reference,
		opts:          opts,
	}
}

// WithReferenceInfo skips probing the reference, for callers scoring many
// encodes of the same source.
func (v *VMAF) WithReferenceInfo(info *VideoInfo) *VMAF {
	v.refInfo = info
	return v
}

// Run scores at the reference's resolution and frame rate. The distorted side
// is scaled (bicubic) and frame-rate converted to match when they differ.
// Both sides are converted to 10-bit 4:2:0 before scoring.
func (v *VMAF) Run(ctx context.Context) (*VMAFResult, error) {
	ref := v.refInfo
	var err error
	if ref == nil {
		if ref, err = ProbeVideo(ctx, v.logger, v.referencePath); err != nil {
			return nil, fmt.Errorf("probe reference: %w", err)
		}
	}
	dist, err := ProbeVideo(ctx, v.logger, v.distortedPath)
	if err != nil {
		return nil, fmt.Errorf("probe distorted: %w", err)
	}

	model := SelectModel(v.opts.Model, ref.Width, ref.Height, ref.FPS)
	refLeg, distLeg := scoreLegs(v.referencePath, v.distortedPath, ref, dist)
	if len(distLeg.filters) > 0 {
		v.logger.Debug("conforming distorted to reference", "filters", distLeg.filters)
	}

	v.logger.Info("running vmaf", "model", ModelName(model), "subsample", max(1, v.opts.Subsample), "distorted", v.distortedPath)
	if err := CheckModelSize(model, ref.Width, ref.Height); err != nil {
		return nil, err
	}

	hint := !v.opts.SkipCambiHint
	out, err := v.score(ctx, model, dist, refLeg, distLeg, hint)
	dropped := false
	if err != nil && strings.Contains(err.Error(), "no such built-in model") {
		return nil, fmt.Errorf("vmaf has no built-in model %q; check -model, or point -vmafmodeldir at the model files: %w", ModelName(model), err)
	}
	if err != nil && hint && isV1Model(model) && isModelLoadError(err) {
		// libvmaf refuses some CAMBI encode sizes: well below the scoring size
		// (a 256x144 rung against 640x360), or small pictures outright (160x240).
		// score without the hint rather than not at all. an older libvmaf fails
		// this retry too and gets the version hint below.
		if out, err = v.score(ctx, model, dist, refLeg, distLeg, false); err == nil {
			dropped = true
			v.logger.Warn("vmaf rejected the encode size for CAMBI, scored without it", "encode", fmt.Sprintf("%dx%d", dist.Width, dist.Height))
		}
	}
	if err != nil && strings.Contains(err.Error(), "image too small") {
		// a model file whose size limit CheckModelSize can't know
		return nil, fmt.Errorf("VMAF v1 model %s needs a larger picture than %dx%d; use -model vmaf_v0.6.1 or a closer-viewing v1 model",
			ModelName(model), ref.Width, ref.Height)
	}
	if err != nil && strings.Contains(err.Error(), "too large for reciprocal LUT") {
		return nil, fmt.Errorf("VMAF v1 model %s can't score a %dx%d picture; scale the source down or use -model vmaf_v0.6.1",
			ModelName(model), ref.Width, ref.Height)
	}
	if err != nil {
		var de *DecodeError
		if isV1Model(model) && ctx.Err() == nil && !errors.As(err, &de) && isModelLoadError(err) {
			return nil, fmt.Errorf("vmaf v1 scoring failed (VMAF v1 needs libvmaf 3.2.1 or newer; set -vmafbin or VMAF_BIN): %w", err)
		}
		return nil, err
	}
	pooled, ok := out.PooledMetrics[vmafMetricName]
	if !ok {
		return nil, fmt.Errorf("vmaf output has no %q pooled metric", vmafMetricName)
	}
	// a picture too small for the model's chroma feature loses it on every
	// frame (below a short side of about 320 for 3d0h, 540 for 5d0h, 640 for
	// 3d0h_2160). black or gray frames lose it only on themselves, so require
	// nearly all of them.
	chromaDropped := len(out.Frames) > 0 &&
		float64(out.chromaDroppedFrames()) >= chromaDroppedShare*float64(len(out.Frames))
	if chromaDropped && !v.opts.QuietChromaWarning {
		v.logger.Warn("VMAF v1's chroma feature couldn't be estimated on nearly every frame (the picture is too small for this model, or its chroma is flat), so this score ignores chroma distortion",
			"reference", fmt.Sprintf("%dx%d", ref.Width, ref.Height))
	}
	if len(out.LegsWithFramesLeft) > 0 {
		msg := fmt.Sprintf("reference and distorted differ in length: the %s still had frames after %d were scored (every %d)",
			strings.Join(out.LegsWithFramesLeft, " and "), len(out.Frames), max(1, v.opts.Subsample))
		if v.opts.StrictFrames {
			return nil, errors.New(msg)
		}
		v.logger.Warn(msg)
	}
	return &VMAFResult{
		CambiHintDropped: dropped,
		ChromaDropped:    chromaDropped,
		Score:            pooled.Mean,
		Model:            ModelName(model),
		MaxScore:         ModelMaxScore(model),
		Frames:           len(out.Frames),
		Subsample:        max(1, v.opts.Subsample),
		Distorted:        dist,
	}, nil
}

func (v *VMAF) score(ctx context.Context, model string, dist *VideoInfo, refLeg, distLeg pipeLeg, cambiHint bool) (*vmafJSON, error) {
	args, err := vmafArgs(v.opts, model, dist, cambiHint)
	if err != nil {
		return nil, err
	}
	return runVMAFPipes(ctx, v.logger, v.opts.bin(), refLeg, distLeg, args)
}

// CheckModelSize refuses a reference too small or too large for a v1 model.
func CheckModelSize(model string, width, height int) error {
	if !isV1Model(model) {
		return nil
	}
	short := v1MinShortSideFor(model)
	if min(width, height) < short || max(width, height) < v1MinLongSide {
		return tooSmallError(model, width, height, short)
	}
	if width+height > v1MaxWidthPlusHeight {
		return fmt.Errorf("VMAF v1 can't score a %dx%d reference (width plus height can be at most %d, as in 7680x4320); scale the source down or use -model vmaf_v0.6.1",
			width, height, v1MaxWidthPlusHeight)
	}
	return nil
}

// v1MinShortSideFor is the smallest short side a v1 model scores. Models for
// farther viewing (phone at 5H, 4K TV at 3H) downsample more in SpEED and
// need a bigger picture; measured with libvmaf 3.2.1.
func v1MinShortSideFor(model string) int {
	name := ModelName(model)
	switch {
	case strings.HasSuffix(name, "_3d0h_2160"):
		return 320 //nolint:mnd
	case strings.HasSuffix(name, "_5d0h"):
		return 270 //nolint:mnd
	default:
		return v1MinShortSide
	}
}

func tooSmallError(model string, width, height, short int) error {
	return fmt.Errorf("VMAF v1 model %s can't score a %dx%d reference (it needs a short side of at least %d and a long side of at least %d); use -model vmaf_v0.6.1 or a closer-viewing v1 model",
		ModelName(model), width, height, short, v1MinLongSide)
}

// isModelLoadError is what an older libvmaf says when it can't run v1.
func isModelLoadError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "feature extractor") || strings.Contains(msg, "problem loading") ||
		strings.Contains(msg, "could not load")
}

func vmafArgs(opts VMAFOptions, model string, dist *VideoInfo, cambiHint bool) ([]string, error) {
	src, err := modelParam(model, opts.ModelDir)
	if err != nil {
		return nil, err
	}
	m := src + ":name=" + vmafMetricName
	if isV1Model(model) && cambiHint {
		// CAMBI judges banding against what the encoder was given, not the
		// 10-bit upscaled picture it's handed for scoring
		m += fmt.Sprintf(":cambi.enc_width=%d:cambi.enc_height=%d:cambi.enc_bitdepth=%d", dist.Width, dist.Height, dist.BitDepth)
	}
	args := []string{"--threads", fmt.Sprint(opts.threads()), "--model", m}
	if opts.Subsample > 1 {
		args = append(args, "--subsample", fmt.Sprint(opts.Subsample))
	}
	return args, nil
}

func scoreLegs(refPath, distPath string, ref, dist *VideoInfo) (refLeg, distLeg pipeLeg) {
	refLeg = pipeLeg{input: refPath, pixFmt: scorePixFmt}
	distLeg = pipeLeg{input: distPath, pixFmt: scorePixFmt}
	if dist.Width != ref.Width || dist.Height != ref.Height {
		distLeg.filters = append(distLeg.filters, fmt.Sprintf("scale=%d:%d:flags=bicubic", ref.Width, ref.Height))
	}
	// only conform two constant-rate files. a variable-rate source and its own
	// encode already match frame for frame, and their averages can still
	// probe apart, so resampling one side would desync the legs.
	if !ref.VFR && !dist.VFR && ref.FPSRatio != "" && math.Abs(dist.FPS-ref.FPS) > 0.001 {
		distLeg.filters = append(distLeg.filters, "fps="+ref.FPSRatio)
	}
	return refLeg, distLeg
}

// CheckVMAF scores two synthetic frames to confirm the vmaf binary can load
// the model opts selects for a 1080p24 source.
func CheckVMAF(ctx context.Context, logger *slog.Logger, opts VMAFOptions) error {
	dir, err := os.MkdirTemp("", "dcrf_vmafcheck_*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir) //nolint:errcheck

	ref := filepath.Join(dir, "ref.mkv")
	dist := filepath.Join(dir, "dist.mkv")
	for _, p := range []struct{ path, src string }{
		{ref, "testsrc2=size=1920x1080:rate=24"},
		{dist, "testsrc2=size=1920x1080:rate=24,boxblur=2"},
	} {
		cmd := exec.CommandContext(ctx, ffmpegBin, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", p.src,
			"-frames:v", "2", "-c:v", "ffv1", "-y", p.path)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("make check clip: %w: %s", err, out)
		}
	}
	_, err = NewVMAF(logger, dist, ref, opts).Run(ctx)
	return err
}
