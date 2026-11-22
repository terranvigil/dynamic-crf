package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
)

// Policy is what the search returns when it can choose between CRFs.
type Policy string

const (
	// PolicyFloor returns the highest CRF (smallest file) whose VMAF is at or
	// above the target. A result below target always says so in its status:
	// target_unreachable, plateau or below_target.
	PolicyFloor Policy = "floor"
	// PolicyClosest returns the CRF whose VMAF is nearest the target, on
	// either side.
	PolicyClosest Policy = "closest"
)

// Status says how a search ended.
type Status string

const (
	// StatusConverged: a score landed inside the tolerance window.
	StatusConverged Status = "converged"
	// StatusBracketed: no grid point is inside the tolerance window, so the
	// result is the better of the two adjacent CRFs straddling the target.
	StatusBracketed Status = "bracketed"
	// StatusTargetExceeded: even the lowest-quality bound beats the target.
	StatusTargetExceeded Status = "target_exceeded_at_bound"
	// StatusUnreachable: even the highest-quality bound misses the target.
	StatusUnreachable Status = "target_unreachable"
	// StatusPlateau: lowering CRF stopped raising bitrate or quality, usually
	// a VBV cap (-maxbitrate), so the target can't be reached.
	StatusPlateau Status = "plateau"
	// StatusBelowTarget: under PolicyFloor the exact score still missed the
	// target after stepping 3 CRF toward quality from the coarse pick. The
	// coarse and exact scores disagree badly; try a lower -subsample.
	StatusBelowTarget Status = "below_target"
)

const (
	// slope of logit(VMAF) per CRF used before two points on one side exist.
	// measured on x264 it runs about -0.1 to -0.25 between CRF 14 and 34.
	priorLogitSlopePerCRF = -0.2
	// a measured slope flatter than this is noise or saturation; use the prior
	minLogitSlopePerCRF = -0.02
	// how close scores must be to count as a quality plateau
	plateauVMAFDelta    = 0.1
	plateauBitrateRatio = 1.02
	// the exact re-score steps toward quality by at most this many CRF
	maxVerifyCRF = 3.0
	// a coarse miss this close to the target gets an exact second look
	nearMissVMAF = 0.5
)

// Score is one evaluation of a CRF.
type Score struct {
	VMAF        float64
	BitrateKbps int
	// MaxScore is the model's top score (100, or 110 for v1's 4K TV model).
	// Zero means 100.
	MaxScore float64
	Model    string
	// CambiHintDropped is set when libvmaf refused CAMBI's encode-size hint
	CambiHintDropped bool
	// ChromaDropped is set when v1's chroma feature couldn't be estimated
	ChromaDropped bool
}

// ScoreFunc encodes at crf (or reuses an earlier encode at that crf) and
// scores it every subsample frames.
type ScoreFunc func(ctx context.Context, crf float64, subsample int) (Score, error)

// SearchParams describes the CRF grid, the target and how to score.
type SearchParams struct {
	TargetVMAF float64
	// Tolerance is the acceptance window in VMAF points. Under PolicyFloor a
	// score in [target, target+tolerance) stops the search; under
	// PolicyClosest anything within tolerance either side does.
	Tolerance float64
	// CRFLow is the numerically lowest CRF searched (highest quality).
	CRFLow float64
	// CRFHigh is the numerically highest CRF searched (lowest quality).
	CRFHigh    float64
	CRFInitial float64
	// CRFStep is the grid spacing. 1 searches integers; 0.5 or 0.25 suit x264
	// and x265, which take fractional CRF.
	CRFStep float64
	Policy  Policy
	// BitrateCapped is set when encodes run under a peak bitrate cap, which
	// enables plateau detection.
	BitrateCapped bool
	// CoarseSubsample scores every Nth frame while searching. The result is
	// then re-scored on every frame. 1 or less scores every frame throughout.
	CoarseSubsample int
}

// SearchResult is the CRF chosen and how it was found.
type SearchResult struct {
	CRF    float64 `json:"crf"`
	VMAF   float64 `json:"vmaf"`
	Status Status  `json:"status"`
	// Exact is true when VMAF was scored on every frame.
	Exact       bool    `json:"exact"`
	BitrateKbps int     `json:"bitrateKbps,omitempty"`
	Model       string  `json:"model,omitempty"`
	TargetVMAF  float64 `json:"targetVmaf"`
	// Scores counts VMAF runs, coarse and exact.
	Scores int `json:"scores"`
	// Encodes counts distinct CRFs encoded.
	Encodes int `json:"encodes"`
	// CambiHintDropped carries the scorer's CAMBI fallback to later scores of
	// the same settings, such as optimize's full encode
	CambiHintDropped bool `json:"-"`
	// ChromaDropped carries the small-picture chroma warning's state the same way
	ChromaDropped bool `json:"-"`
}

// Validate checks the grid, target and policy.
func (p *SearchParams) Validate() error {
	for name, v := range map[string]float64{
		"target vmaf": p.TargetVMAF, "tolerance": p.Tolerance, "crf low bound": p.CRFLow,
		"crf high bound": p.CRFHigh, "initial crf": p.CRFInitial, "crf step": p.CRFStep,
	} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("%s must be a finite number", name)
		}
	}
	if p.CRFStep <= 0 {
		return fmt.Errorf("crf step (%g) must be positive", p.CRFStep)
	}
	if p.CRFLow <= 0 {
		return fmt.Errorf("crf low bound (%g) must be positive", p.CRFLow)
	}
	if p.CRFHigh <= p.CRFLow {
		return fmt.Errorf("crf high bound (%g) must be greater than crf low bound (%g)", p.CRFHigh, p.CRFLow)
	}
	if p.CRFHigh-p.CRFLow < p.CRFStep {
		return fmt.Errorf("crf range %g..%g is narrower than the step %g", p.CRFLow, p.CRFHigh, p.CRFStep)
	}
	// the grid must land on both bounds, or the high bound is never tried
	if steps := (p.CRFHigh - p.CRFLow) / p.CRFStep; math.Abs(steps-math.Round(steps)) > 1e-6 {
		return fmt.Errorf("crf range %g..%g isn't a whole number of %g steps", p.CRFLow, p.CRFHigh, p.CRFStep)
	}
	if p.CRFInitial < p.CRFLow || p.CRFInitial > p.CRFHigh {
		return fmt.Errorf("initial crf (%g) must be between %g and %g", p.CRFInitial, p.CRFLow, p.CRFHigh)
	}
	if p.TargetVMAF <= 0 || p.TargetVMAF >= 110 {
		return fmt.Errorf("target vmaf (%.2f) must be above 0 and below the model's maximum", p.TargetVMAF)
	}
	if p.Tolerance <= 0 {
		return fmt.Errorf("tolerance (%.2f) must be positive", p.Tolerance)
	}
	if p.Policy != PolicyFloor && p.Policy != PolicyClosest {
		return fmt.Errorf("policy %q must be %q or %q", p.Policy, PolicyFloor, PolicyClosest)
	}
	return nil
}

// gridSearch runs the search over grid indices: index i is CRF CRFLow+i*CRFStep,
// so VMAF falls as i rises. It keeps the largest index known to meet the
// target (good) and the smallest known to miss it (bad). Once both exist it
// takes secant steps between them in logit(VMAF) space, where the curve is
// much closer to linear than raw VMAF, falling back to bisection when one side
// keeps moving. Before the target is bracketed it steps from the frontier
// using a measured or prior slope, doubling the step cap each time.
//
// every probe lands strictly between good and bad, or beyond the only side
// known, so no index is ever scored twice and the loop ends within n+1 probes.
type gridSearch struct {
	logger  *slog.Logger
	p       SearchParams
	score   ScoreFunc
	n       int // highest grid index
	coarse  map[int]Score
	exact   map[int]Score
	encoded map[int]bool
	scores  int
	maxV    float64
	// cambiDropped is set once any score needed the CAMBI fallback
	cambiDropped bool
	// chromaDropped is set once any score couldn't estimate chroma
	chromaDropped bool
}

func newGridSearch(logger *slog.Logger, p SearchParams, score ScoreFunc) *gridSearch {
	return &gridSearch{
		logger:  logger,
		p:       p,
		score:   score,
		n:       int(math.Floor((p.CRFHigh-p.CRFLow)/p.CRFStep + 1e-9)),
		coarse:  map[int]Score{},
		exact:   map[int]Score{},
		encoded: map[int]bool{},
		maxV:    100, //nolint:mnd
	}
}

func (g *gridSearch) crf(i int) float64 {
	// round off float drift so 0.1-style steps format cleanly
	return math.Round((g.p.CRFLow+float64(i)*g.p.CRFStep)*1e6) / 1e6
}

func (g *gridSearch) index(crf float64) int {
	i := int(math.Round((crf - g.p.CRFLow) / g.p.CRFStep))
	return min(max(i, 0), g.n)
}

func (g *gridSearch) coarseSubsample() int {
	return max(1, g.p.CoarseSubsample)
}

func (g *gridSearch) eval(ctx context.Context, i int, exact bool) (Score, error) {
	cache, sub := g.coarse, g.coarseSubsample()
	if exact {
		cache, sub = g.exact, 1
	}
	if s, ok := cache[i]; ok {
		return s, nil
	}
	s, err := g.score(ctx, g.crf(i), sub)
	if err != nil {
		return Score{}, err
	}
	if math.IsNaN(s.VMAF) || math.IsInf(s.VMAF, 0) {
		return Score{}, fmt.Errorf("crf %g scored a non-finite vmaf %v", g.crf(i), s.VMAF)
	}
	g.scores++
	g.encoded[i] = true
	g.cambiDropped = g.cambiDropped || s.CambiHintDropped
	g.chromaDropped = g.chromaDropped || s.ChromaDropped
	if s.MaxScore > 0 {
		g.maxV = s.MaxScore
	}
	cache[i] = s
	if g.coarseSubsample() == 1 {
		// coarse and exact are the same run when every frame is scored
		g.exact[i], g.coarse[i] = s, s
	}
	g.logger.Info("scored", "crf", g.crf(i), "vmaf", fmt.Sprintf("%.3f", s.VMAF), "subsample", sub, "model", s.Model, "bitrateKbps", s.BitrateKbps)
	return s, nil
}

// logit maps VMAF onto a scale where the CRF curve is close to linear.
func (g *gridSearch) logit(v float64) float64 {
	c := min(max(v, 1), g.maxV-0.1) //nolint:mnd
	return math.Log(c / (g.maxV - c))
}

func (g *gridSearch) accept(v float64) bool {
	t := g.p.TargetVMAF
	if g.p.Policy == PolicyClosest {
		return math.Abs(v-t) < g.p.Tolerance
	}
	return v >= t && v < t+g.p.Tolerance
}

// slope estimates d logit / d index from the two scored points nearest the
// frontier on one side, or falls back to the prior.
func (g *gridSearch) slope(a, b int) float64 {
	prior := priorLogitSlopePerCRF * g.p.CRFStep
	if a < 0 || b < 0 || a == b {
		return prior
	}
	k := (g.logit(g.coarse[b].VMAF) - g.logit(g.coarse[a].VMAF)) / float64(b-a)
	if k > minLogitSlopePerCRF*g.p.CRFStep {
		return prior
	}
	return k
}

func (g *gridSearch) run(ctx context.Context) (SearchResult, error) {
	i := g.index(g.p.CRFInitial)
	good, bad := -1, -1
	// second-nearest points to the frontier on each side, for slope estimates
	good2, bad2 := -1, -1
	lastSide, sameSide := 0, 0
	stepCap := g.n

	for range g.n + 2 {
		if _, seen := g.coarse[i]; seen {
			return SearchResult{}, fmt.Errorf("internal: search revisited crf %g", g.crf(i))
		}
		s, err := g.eval(ctx, i, false)
		if err != nil {
			return SearchResult{}, err
		}
		if g.p.TargetVMAF >= g.maxV {
			return SearchResult{}, fmt.Errorf("target vmaf %.2f must be below model %s's maximum of %g", g.p.TargetVMAF, s.Model, g.maxV)
		}
		if g.accept(s.VMAF) {
			return g.finish(ctx, i, StatusConverged)
		}

		side := -1
		if s.VMAF >= g.p.TargetVMAF {
			side = 1
			if i > good {
				good, good2 = i, good
			} else if i > good2 {
				good2 = i
			}
		} else {
			if bad < 0 || i < bad {
				bad, bad2 = i, bad
			} else if bad2 < 0 || i < bad2 {
				bad2 = i
			}
		}
		if side == lastSide {
			sameSide++
		} else {
			sameSide = 1
		}
		lastSide = side

		switch {
		case good >= 0 && bad >= 0:
			if bad-good <= 1 {
				return g.finish(ctx, g.pickAdjacent(good, bad), StatusBracketed)
			}
			i = g.secant(good, bad, sameSide)
		case good >= 0:
			// everything scored so far meets the target: trade quality for size
			if good == g.n {
				return g.finish(ctx, g.n, StatusTargetExceeded)
			}
			yT := g.logit(g.p.TargetVMAF)
			d := (yT - g.logit(g.coarse[good].VMAF)) / g.slope(good2, good)
			step := min(max(int(math.Floor(d)), 1), stepCap)
			stepCap = 2 * step
			i = min(good+step, g.n)
		default:
			// everything scored so far misses the target: spend bits
			if bad == 0 {
				return g.finish(ctx, 0, StatusUnreachable)
			}
			if g.plateau(bad, bad2) {
				g.logger.Warn("quality stopped improving as crf fell, likely capped by -maxbitrate", "crf", g.crf(bad))
				return g.finish(ctx, bad, StatusPlateau)
			}
			yT := g.logit(g.p.TargetVMAF)
			d := (yT - g.logit(g.coarse[bad].VMAF)) / -g.slope(bad, bad2)
			step := min(max(int(math.Ceil(d)), 1), stepCap)
			stepCap = 2 * step
			i = max(bad-step, 0)
		}
	}
	return SearchResult{}, errors.New("internal: search did not terminate")
}

// secant interpolates the target crossing between good and bad in logit
// space. If the same side has moved three times running the interpolation is
// stalling against curvature, so it bisects instead (the Illinois idea).
func (g *gridSearch) secant(good, bad, sameSide int) int {
	yg, yb := g.logit(g.coarse[good].VMAF), g.logit(g.coarse[bad].VMAF)
	next := (good + bad) / 2
	if sameSide < 3 && yg > yb {
		root := float64(good) + (yg-g.logit(g.p.TargetVMAF))/(yg-yb)*float64(bad-good)
		next = int(math.Floor(root))
	}
	return min(max(next, good+1), bad-1)
}

func (g *gridSearch) pickAdjacent(good, bad int) int {
	if g.p.Policy == PolicyClosest {
		t := g.p.TargetVMAF
		if math.Abs(g.coarse[bad].VMAF-t) < math.Abs(g.coarse[good].VMAF-t) {
			return bad
		}
	}
	return good
}

// plateau reports whether moving from bad2 down to the higher-quality bad
// bought neither bitrate nor quality. Only a bitrate cap does that, so it's
// only checked when one is set, and only across at least one CRF: with fine
// steps two neighbors differ by less than the thresholds anyway.
func (g *gridSearch) plateau(bad, bad2 int) bool {
	if !g.p.BitrateCapped || bad2 < 0 || g.crf(bad2)-g.crf(bad) < 1 {
		return false
	}
	hi, lo := g.coarse[bad], g.coarse[bad2]
	if hi.BitrateKbps <= 0 || lo.BitrateKbps <= 0 {
		return false
	}
	return float64(hi.BitrateKbps) <= plateauBitrateRatio*float64(lo.BitrateKbps) &&
		hi.VMAF-lo.VMAF < plateauVMAFDelta
}

// finish re-scores the chosen CRF on every frame and sets the status from
// that exact score. Under PolicyFloor it also corrects for the coarse pass:
// if the exact score misses the target it steps toward quality, and if it
// meets it, climb checks whether the next CRF up does too.
func (g *gridSearch) finish(ctx context.Context, i int, status Status) (SearchResult, error) {
	s, err := g.eval(ctx, i, true)
	if err != nil {
		return SearchResult{}, err
	}
	t := g.p.TargetVMAF
	if g.p.Policy == PolicyFloor {
		switch {
		case s.VMAF < t && status != StatusUnreachable && status != StatusPlateau:
			budget := max(1, int(math.Ceil(maxVerifyCRF/g.p.CRFStep)))
			for step := 0; s.VMAF < t && step < budget && i > 0; step++ {
				g.logger.Info("exact score below target, stepping toward quality", "crf", g.crf(i), "vmaf", fmt.Sprintf("%.3f", s.VMAF))
				i--
				if s, err = g.eval(ctx, i, true); err != nil {
					return SearchResult{}, err
				}
			}
		case s.VMAF >= t && g.coarseSubsample() > 1:
			// includes a coarse "unreachable" or "plateau" that the exact score
			// overturned: higher CRFs may meet the target too
			if i, s, err = g.climb(ctx, i, s); err != nil {
				return SearchResult{}, err
			}
		}
	}
	return SearchResult{
		CRF:         g.crf(i),
		VMAF:        s.VMAF,
		Status:      g.exactStatus(i, s.VMAF, status),
		Exact:       true,
		BitrateKbps: s.BitrateKbps,
		Model:       s.Model,
		TargetVMAF:  t,
		Scores:      g.scores,
		Encodes:     len(g.encoded),

		CambiHintDropped: g.cambiDropped,
		ChromaDropped:    g.chromaDropped,
	}, nil
}

// climb moves up the grid while the next CRF also meets the target on every
// frame. It looks when the coarse pass rejected the next CRF by only a hair,
// or never scored it and the exact score sits above the tolerance window.
// Under PolicyFloor either way may hide a smaller file.
func (g *gridSearch) climb(ctx context.Context, i int, s Score) (int, Score, error) {
	t := g.p.TargetVMAF
	budget := max(1, int(math.Ceil(maxVerifyCRF/g.p.CRFStep)))
	for range budget {
		if i >= g.n {
			break
		}
		next, scored := g.coarse[i+1]
		if scored && next.VMAF < t-nearMissVMAF {
			break
		}
		if !scored && s.VMAF < t+g.p.Tolerance {
			break
		}
		up, err := g.eval(ctx, i+1, true)
		if err != nil {
			return i, s, err
		}
		if up.VMAF < t {
			break
		}
		g.logger.Info("next crf meets target on every frame", "crf", g.crf(i+1), "vmaf", fmt.Sprintf("%.3f", up.VMAF))
		i, s = i+1, up
	}
	return i, s, nil
}

// exactStatus says how the search ended from the exact score and where it
// sits on the grid. The coarse status only matters for a plateau, which the
// score alone can't show.
func (g *gridSearch) exactStatus(i int, v float64, status Status) Status {
	meets := v >= g.p.TargetVMAF
	switch {
	case g.accept(v):
		return StatusConverged
	case !meets && i == 0:
		return StatusUnreachable
	case !meets && status == StatusPlateau:
		return StatusPlateau
	case !meets && g.p.Policy == PolicyFloor:
		g.logger.Warn("exact score still below target after stepping toward quality", "crf", g.crf(i), "vmaf", fmt.Sprintf("%.3f", v))
		return StatusBelowTarget
	case meets && i == g.n:
		return StatusTargetExceeded
	default:
		return StatusBracketed
	}
}
