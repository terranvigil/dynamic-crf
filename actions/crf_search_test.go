//go:build unit

package actions

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terranvigil/dynamic-crf/commands"
)

var quiet = slog.New(slog.DiscardHandler)

// curve maps CRF to the true (every-frame) VMAF score.
type curve func(crf float64) float64

// logistic is exactly linear in logit space: VMAF = 100 / (1 + exp(k*(crf-mid))).
func logistic(k, mid float64) curve {
	return func(crf float64) float64 { return 100 / (1 + math.Exp(k*(crf-mid))) }
}

// expDistortion saturates differently from a logistic: 100 - a*exp(g*(crf-c0)).
func expDistortion(a, g, c0 float64) curve {
	return func(crf float64) float64 { return math.Max(0, 100-a*math.Exp(g*(crf-c0))) }
}

// bbbPoints is Big Buck Bunny 1080p, 8s, libx264 medium, scored on v0.6.1.
var bbbPoints = [][2]float64{{14, 97.54}, {18, 96.75}, {22, 95.32}, {26, 92.58}, {30, 87.44}, {34, 78.94}, {42, 48.82}}

// measured interpolates bbbPoints in logit space, shifted by shift CRF.
func measured(shift float64) curve {
	lg := func(v float64) float64 { return math.Log(v / (100 - v)) }
	inv := func(y float64) float64 { return 100 / (1 + math.Exp(-y)) }
	return func(crf float64) float64 {
		c := crf - shift
		pts := bbbPoints
		i := 0
		for i < len(pts)-2 && c > pts[i+1][0] {
			i++
		}
		a, b := pts[i], pts[i+1]
		t := (c - a[0]) / (b[0] - a[0])
		return inv(lg(a[1]) + t*(lg(b[1])-lg(a[1])))
	}
}

// fakeScorer stands in for encode+score. It fails the test if the same CRF is
// scored twice at the same subsample or a CRF outside the bounds is asked for.
type fakeScorer struct {
	t        *testing.T
	truth    curve
	low      float64
	high     float64
	maxScore float64
	// noise adds a deterministic per-CRF offset of up to +-noise to coarse scores
	noise float64
	// coarseBias is added to coarse scores, to model subsampling flattering a clip
	coarseBias float64
	// bitrate returns kbps for a crf; nil means a plain exponential
	bitrate func(crf float64) int

	calls  map[string]int
	coarse int
	exact  int
}

func newFake(t *testing.T, truth curve, low, high float64) *fakeScorer {
	t.Helper()
	return &fakeScorer{t: t, truth: truth, low: low, high: high, calls: map[string]int{}}
}

func (f *fakeScorer) score(_ context.Context, crf float64, subsample int) (Score, error) {
	f.t.Helper()
	key := fmt.Sprintf("%g@%d", crf, subsample)
	f.calls[key]++
	require.Equal(f.t, 1, f.calls[key], "crf %g scored twice at subsample %d", crf, subsample)
	require.GreaterOrEqual(f.t, crf, f.low, "crf below bounds")
	require.LessOrEqual(f.t, crf, f.high, "crf above bounds")

	v := f.truth(crf)
	if subsample > 1 {
		f.coarse++
		v += f.coarseBias
		if f.noise > 0 {
			h := fnv.New32a()
			_, _ = fmt.Fprintf(h, "%g", crf)
			v += f.noise * (2*float64(h.Sum32()%1000)/999 - 1)
		}
	} else {
		f.exact++
	}
	kbps := int(8000 * math.Pow(0.85, crf-15))
	if f.bitrate != nil {
		kbps = f.bitrate(crf)
	}
	return Score{VMAF: v, BitrateKbps: kbps, MaxScore: f.maxScore, Model: "fake"}, nil
}

func params(target, tol, low, high, initial float64) SearchParams {
	return SearchParams{
		TargetVMAF:      target,
		Tolerance:       tol,
		CRFLow:          low,
		CRFHigh:         high,
		CRFInitial:      initial,
		CRFStep:         1,
		Policy:          PolicyFloor,
		CoarseSubsample: DefaultCoarseSubsample,
	}
}

func runSearch(t *testing.T, p SearchParams, f *fakeScorer) SearchResult {
	t.Helper()
	s := &CrfSearch{logger: quiet, sourcePath: "/dev/null", params: p, scoreFn: f.score}
	res, err := s.Run(context.Background())
	require.NoError(t, err)
	require.True(t, res.Exact, "result should be re-scored on every frame")
	assertStatusMatchesScore(t, p, res)
	assert.GreaterOrEqual(t, res.CRF, p.CRFLow)
	assert.LessOrEqual(t, res.CRF, p.CRFHigh)
	return res
}

// assertStatusMatchesScore checks the status is true of the exact score.
func assertStatusMatchesScore(t *testing.T, p SearchParams, res SearchResult) {
	t.Helper()
	switch res.Status {
	case StatusConverged:
		if p.Policy == PolicyFloor {
			assert.True(t, res.VMAF >= p.TargetVMAF && res.VMAF < p.TargetVMAF+p.Tolerance, "converged outside window: %+v", res)
		} else {
			assert.Less(t, math.Abs(res.VMAF-p.TargetVMAF), p.Tolerance, "converged outside window: %+v", res)
		}
	case StatusBracketed, StatusTargetExceeded:
		if p.Policy == PolicyFloor {
			assert.GreaterOrEqual(t, res.VMAF, p.TargetVMAF, "%s below target: %+v", res.Status, res)
		}
	case StatusBelowTarget:
		assert.Less(t, res.VMAF, p.TargetVMAF)
	case StatusUnreachable:
		assert.InDelta(t, p.CRFLow, res.CRF, 1e-9, "unreachable away from crf-low: %+v", res)
		assert.Less(t, res.VMAF, p.TargetVMAF)
	}
	if res.Status == StatusTargetExceeded {
		assert.InDelta(t, p.CRFHigh, res.CRF, 1e-9, "exceeded away from crf-high: %+v", res)
		assert.GreaterOrEqual(t, res.VMAF, p.TargetVMAF)
	}
}

// assertFloorOptimal checks the floor contract: the result meets the target
// and the next CRF up the grid doesn't (or the result is the top bound).
func assertFloorOptimal(t *testing.T, p SearchParams, truth curve, res SearchResult) {
	t.Helper()
	assert.GreaterOrEqual(t, res.VMAF, p.TargetVMAF, "floor result must meet target")
	assert.InDelta(t, truth(res.CRF), res.VMAF, 1e-9, "reported vmaf should be the exact score")
	if res.CRF+p.CRFStep <= p.CRFHigh {
		assert.Less(t, truth(res.CRF+p.CRFStep), p.TargetVMAF, "crf %g is not the highest meeting target", res.CRF)
	}
}

func TestSearch_Logistic(t *testing.T) {
	truth := logistic(0.3, 28)
	tests := []struct {
		name               string
		target, tol        float64
		low, high, initial float64
	}{
		{"target 95 plateau", 95, 0.5, 10, 35, 20},
		{"target 90", 90, 0.5, 10, 35, 20},
		{"target 80 steep", 80, 0.5, 10, 40, 25},
		{"target 95 narrow range", 95, 0.5, 15, 25, 20},
		{"initial at high bound", 93, 0.5, 15, 30, 30},
		{"initial at low bound", 93, 0.5, 15, 30, 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := params(tt.target, tt.tol, tt.low, tt.high, tt.initial)
			f := newFake(t, truth, tt.low, tt.high)
			res := runSearch(t, p, f)
			assertFloorOptimal(t, p, truth, res)
			// the logistic is a straight line in logit space, so the secant
			// should land within a few probes
			assert.LessOrEqual(t, f.coarse, 4, "coarse probes")
			assert.LessOrEqual(t, f.exact, 2, "exact re-scores")
			t.Logf("crf=%g vmaf=%.2f status=%s coarse=%d exact=%d", res.CRF, res.VMAF, res.Status, f.coarse, f.exact)
		})
	}
}

func TestSearch_Linear(t *testing.T) {
	truth := func(crf float64) float64 { return 100 - 2*crf }
	p := params(60, 0.5, 15, 30, 18)
	f := newFake(t, truth, 15, 30)
	res := runSearch(t, p, f)
	assert.InDelta(t, 20, res.CRF, 0)
	assert.Equal(t, StatusConverged, res.Status)
}

// the old search returned CRF 0 when even the low-quality bound beat the target
func TestSearch_TargetExceededAtBound(t *testing.T) {
	truth := func(crf float64) float64 { return 100 - 0.15*crf } // 95.5 at crf 30
	p := params(90, 0.5, 15, 30, 20)
	f := newFake(t, truth, 15, 30)
	res := runSearch(t, p, f)
	assert.InDelta(t, 30, res.CRF, 0)
	assert.Equal(t, StatusTargetExceeded, res.Status)
	assert.Greater(t, res.VMAF, 90.0)
}

func TestSearch_Unreachable(t *testing.T) {
	truth := logistic(0.3, 22) // 97.3 at crf 10
	p := params(99, 0.5, 10, 30, 20)
	f := newFake(t, truth, 10, 30)
	res := runSearch(t, p, f)
	assert.InDelta(t, 10, res.CRF, 0)
	assert.Equal(t, StatusUnreachable, res.Status)
	assert.InDelta(t, truth(10), res.VMAF, 1e-9)
}

// the old search returned CRF 0 when the initial CRF left a bracket one wide
func TestSearch_OneWideBracket(t *testing.T) {
	truth := logistic(0.3, 28)
	for _, tt := range []struct {
		name               string
		target             float64
		low, high, initial float64
	}{
		{"initial next to high bound", truth(29.5), 15, 30, 29},
		{"initial next to low bound", truth(15.5), 15, 30, 16},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := params(tt.target, 0.01, tt.low, tt.high, tt.initial)
			f := newFake(t, truth, tt.low, tt.high)
			res := runSearch(t, p, f)
			assert.NotZero(t, res.CRF)
			assertFloorOptimal(t, p, truth, res)
			assert.Equal(t, StatusBracketed, res.Status)
		})
	}
}

// the old search returned the below-target side when no integer CRF landed
// inside the tolerance
func TestSearch_NoGridPointInTolerance(t *testing.T) {
	truth := measured(0)
	for target := 90.0; target <= 96.5; target += 0.25 {
		t.Run(fmt.Sprintf("target %.2f", target), func(t *testing.T) {
			p := params(target, 0.1, 15, 30, 20)
			f := newFake(t, truth, 15, 30)
			res := runSearch(t, p, f)
			assertFloorOptimal(t, p, truth, res)
		})
	}
}

func TestSearch_ClosestPolicy(t *testing.T) {
	truth := logistic(0.3, 28)
	// crf 23 scores 81.76 and crf 24 scores 76.85: 23 is nearer 80
	p := params(80, 0.5, 10, 40, 25)
	p.Policy = PolicyClosest
	f := newFake(t, truth, 10, 40)
	res := runSearch(t, p, f)
	assert.InDelta(t, 23, res.CRF, 0)

	// crf 27 scores 57.4, crf 28 scores 50: 28 is nearer 52 even though it's below
	p = params(52, 0.5, 10, 40, 25)
	p.Policy = PolicyClosest
	f = newFake(t, truth, 10, 40)
	res = runSearch(t, p, f)
	assert.InDelta(t, 28, res.CRF, 0)
	assert.Less(t, res.VMAF, 52.0)
}

func TestSearch_FractionalStep(t *testing.T) {
	truth := measured(0)
	p := params(94, 0.1, 15, 30, 20)
	p.CRFStep = 0.5
	f := newFake(t, truth, 15, 30)
	res := runSearch(t, p, f)
	assertFloorOptimal(t, p, truth, res)
	assert.InDelta(t, 0, math.Mod(res.CRF, 0.5), 1e-9, "result should be on the 0.5 grid")

	p.CRFStep = 0.25
	f = newFake(t, truth, 15, 30)
	res = runSearch(t, p, f)
	assertFloorOptimal(t, p, truth, res)
	assert.InDelta(t, 0, math.Mod(res.CRF, 0.25), 1e-9)
}

// subsampling that flatters the clip is caught by the exact re-score, which
// steps toward quality until the target is met
func TestSearch_ExactVerificationStepsDown(t *testing.T) {
	truth := measured(0)
	p := params(93, 0.5, 15, 30, 20)
	f := newFake(t, truth, 15, 30)
	f.coarseBias = 0.9
	res := runSearch(t, p, f)
	assertFloorOptimal(t, p, truth, res)
	assert.GreaterOrEqual(t, f.exact, 2, "should have re-scored more than once")
}

// the coarse pass rejecting the next CRF by a hair shouldn't cost the
// smaller file: it gets an exact score from its cached encode
func TestSearch_NearMissNeighborGetsExactScore(t *testing.T) {
	truth := measured(0)
	// pick a target just under the true score at crf 21
	target := truth(21) - 0.05
	p := params(target, 0.01, 15, 30, 18)
	f := newFake(t, truth, 15, 30)
	f.coarseBias = -0.2
	res := runSearch(t, p, f)
	assert.InDelta(t, 21, res.CRF, 0, "%+v", res)
	assertFloorOptimal(t, p, truth, res)
}

// when coarse scores flatter the clip so much that 3 CRF of stepping can't
// recover, the result says so instead of passing as bracketed
func TestSearch_BelowTargetStatus(t *testing.T) {
	truth := measured(0)
	p := params(93, 0.5, 15, 30, 20)
	p.CRFStep = 0.25
	f := newFake(t, truth, 15, 30)
	f.coarseBias = 6
	res := runSearch(t, p, f)
	assert.Equal(t, StatusBelowTarget, res.Status, "%+v", res)
	assert.Less(t, res.VMAF, 93.0)
	assert.Equal(t, 12+1, f.exact, "one exact score plus 3 CRF of 0.25 steps")
}

// a coarse pass that reads low accepts a CRF whose exact score sits above
// the window; the next CRF up was never scored and may also meet the target
func TestSearch_ClimbsWhenExactAboveWindow(t *testing.T) {
	truth := measured(0)
	for _, target := range []float64{92, 93, 94} {
		// coarse reads 0.4 low against a 0.3 window, so every coarse accept is
		// an exact score above the window
		p := params(target, 0.3, 15, 30, 20)
		f := newFake(t, truth, 15, 30)
		f.coarseBias = -0.4
		res := runSearch(t, p, f)
		assertFloorOptimal(t, p, truth, res)
	}
}

// the lowest bound missing on the coarse pass but meeting on every frame
// isn't unreachable
func TestSearch_UnreachableRecheckedExactly(t *testing.T) {
	truth := func(crf float64) float64 { return 95.1 - 0.5*(crf-15) }
	p := params(95, 0.5, 15, 30, 20)
	f := newFake(t, truth, 15, 30)
	f.coarseBias = -0.3
	res := runSearch(t, p, f)
	assert.InDelta(t, 15, res.CRF, 0)
	assert.Equal(t, StatusConverged, res.Status, "%+v", res)
}

// under closest, exact scores revise coarse verdicts at the bounds too
func TestSearch_ClosestStatusesFromExactScore(t *testing.T) {
	// coarse misses at crf-low but exact lands above the window
	truth := func(crf float64) float64 { return 95.6 - 0.5*(crf-15) }
	p := params(95, 0.5, 15, 30, 20)
	p.Policy = PolicyClosest
	f := newFake(t, truth, 15, 30)
	f.coarseBias = -1.2
	res := runSearch(t, p, f)
	assert.NotEqual(t, StatusUnreachable, res.Status, "%+v", res)

	// coarse meets at crf-high but exact falls short
	truth = func(crf float64) float64 { return 98 - 0.01*crf }
	p = params(97.9, 0.05, 15, 30, 20)
	p.Policy = PolicyClosest
	f = newFake(t, truth, 15, 30)
	f.coarseBias = 0.4
	res = runSearch(t, p, f)
	if res.CRF == 30 && res.VMAF < p.TargetVMAF {
		assert.NotEqual(t, StatusTargetExceeded, res.Status, "%+v", res)
	}
}

// bound statuses come from the exact score, whatever the coarse pass said
func TestSearch_BoundStatusesFromExactScore(t *testing.T) {
	tests := []struct {
		name    string
		policy  Policy
		truth   curve
		bias    float64
		initial float64
		want    Status
		crf     float64
	}{
		// coarse 94.6 at crf 15 is in the closest window; exact 94.4 isn't
		{"closest low bound misses", PolicyClosest, func(c float64) float64 { return 94.4 - 0.5*(c-15) }, 0.2, 15, StatusUnreachable, 15},
		// coarse 95.4 at crf 30 converges; exact 95.6 sits above the window
		{"floor high bound exceeds", PolicyFloor, func(c float64) float64 { return 95.6 + 0.01*(30-c) }, -0.2, 30, StatusTargetExceeded, 30},
		{"closest high bound exceeds", PolicyClosest, func(c float64) float64 { return 95.6 + 0.01*(30-c) }, -0.2, 30, StatusTargetExceeded, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := params(95, 0.5, 15, 30, tt.initial)
			p.Policy = tt.policy
			f := newFake(t, tt.truth, 15, 30)
			f.coarseBias = tt.bias
			res := runSearch(t, p, f)
			assert.Equal(t, tt.want, res.Status, "%+v", res)
			assert.InDelta(t, tt.crf, res.CRF, 0)
		})
	}

	// coarse unreachable at crf 15, but exact meets there and at 16
	truth := func(c float64) float64 { return 95.5 - 0.3*(c-15) }
	p := params(95, 0.5, 15, 30, 20)
	f := newFake(t, truth, 15, 30)
	f.coarseBias = -0.6
	res := runSearch(t, p, f)
	assertFloorOptimal(t, p, truth, res)
}

func TestSearch_NoisyScores(t *testing.T) {
	for _, shift := range []float64{-4, -2, 0, 2, 4} {
		truth := measured(shift)
		for _, target := range []float64{91, 93, 95} {
			t.Run(fmt.Sprintf("shift %g target %g", shift, target), func(t *testing.T) {
				p := params(target, 0.5, 15, 30, 20)
				f := newFake(t, truth, 15, 30)
				f.noise = 0.25
				res := runSearch(t, p, f)
				// noise can cost optimality but never the target
				if res.Status != StatusUnreachable {
					assert.GreaterOrEqual(t, res.VMAF, target)
				}
			})
		}
	}
}

// a VBV cap stops bitrate and quality rising as CRF falls
func TestSearch_Plateau(t *testing.T) {
	capCRF := 24.0
	truth := func(crf float64) float64 { return measured(4)(math.Max(crf, capCRF)) }
	p := params(97, 0.5, 15, 30, 26)
	p.BitrateCapped = true
	f := newFake(t, truth, 15, 30)
	f.bitrate = func(crf float64) int { return int(8000 * math.Pow(0.85, math.Max(crf, capCRF)-15)) }
	res := runSearch(t, p, f)
	assert.Equal(t, StatusPlateau, res.Status)
	assert.Less(t, f.coarse, 16, "should stop before scanning the range")
}

// fine steps used to look like a plateau: two neighbors 0.1 CRF apart differ
// by less than the bitrate and vmaf thresholds even with no cap
func TestSearch_NoFalsePlateauOnFineSteps(t *testing.T) {
	// crosses 95 just below the first probe, so the next probes are 0.1 apart
	truth := func(crf float64) float64 { return 95 - 0.6*(crf-19.86) }
	for _, capped := range []bool{false, true} {
		p := params(95, 0.01, 1, 30, 20)
		p.CRFStep = 0.1
		p.BitrateCapped = capped
		f := newFake(t, truth, 1, 30)
		f.bitrate = func(crf float64) int { return int(8000 * math.Pow(2, -crf/6)) }
		res := runSearch(t, p, f)
		assert.NotEqual(t, StatusPlateau, res.Status, "capped=%v %+v", capped, res)
		assertFloorOptimal(t, p, truth, res)
	}
}

func TestSearch_NonFiniteRejected(t *testing.T) {
	p := params(95, 0.5, 15, 30, 20)
	p.TargetVMAF = math.NaN()
	s := &CrfSearch{logger: quiet, params: p, scoreFn: func(context.Context, float64, int) (Score, error) { return Score{}, nil }}
	_, err := s.Run(context.Background())
	require.ErrorContains(t, err, "finite")

	p = params(95, 0.5, 15, 30, 20)
	s = &CrfSearch{logger: quiet, params: p, scoreFn: func(context.Context, float64, int) (Score, error) {
		return Score{VMAF: math.NaN()}, nil
	}}
	_, err = s.Run(context.Background())
	require.ErrorContains(t, err, "non-finite")
}

func TestSearch_MaxScore110(t *testing.T) {
	// v1's 4K TV model tops out at 110
	truth := func(crf float64) float64 { return 110 / (1 + math.Exp(0.3*(crf-30))) }
	p := params(104, 0.5, 15, 35, 20)
	f := newFake(t, truth, 15, 35)
	f.maxScore = 110
	res := runSearch(t, p, f)
	assertFloorOptimal(t, p, truth, res)

	// a 100-point model can't be asked for 100 or more
	p = params(100, 0.5, 15, 35, 20)
	f = newFake(t, logistic(0.3, 28), 15, 35)
	s := &CrfSearch{logger: quiet, params: p, scoreFn: f.score}
	_, err := s.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum")
}

func TestSearch_ScoreErrorPropagates(t *testing.T) {
	boom := errors.New("encode failed")
	s := &CrfSearch{logger: quiet, params: params(93, 0.5, 15, 30, 20), scoreFn: func(context.Context, float64, int) (Score, error) {
		return Score{}, boom
	}}
	_, err := s.Run(context.Background())
	require.ErrorIs(t, err, boom)
}

func TestSearch_EverySubsampleOneScoresOnce(t *testing.T) {
	truth := measured(0)
	p := params(93, 0.5, 15, 30, 20)
	p.CoarseSubsample = 1
	f := newFake(t, truth, 15, 30)
	res := runSearch(t, p, f)
	assertFloorOptimal(t, p, truth, res)
	assert.Zero(t, f.coarse)
	assert.Equal(t, res.Scores, f.exact, "with subsample 1 the result isn't re-scored")
}

func TestSearch_Validation(t *testing.T) {
	ok := params(95, 0.5, 15, 30, 20)
	tests := []struct {
		name   string
		mutate func(p *SearchParams)
		want   string
	}{
		{"high <= low", func(p *SearchParams) { p.CRFLow, p.CRFHigh = 30, 15 }, "greater than crf low"},
		{"low not positive", func(p *SearchParams) { p.CRFLow = 0 }, "must be positive"},
		{"target too high", func(p *SearchParams) { p.TargetVMAF = 111 }, "target vmaf"},
		{"target zero", func(p *SearchParams) { p.TargetVMAF = 0 }, "target vmaf"},
		{"zero tolerance", func(p *SearchParams) { p.Tolerance = 0 }, "tolerance"},
		{"initial out of range", func(p *SearchParams) { p.CRFInitial = 5 }, "initial crf"},
		{"zero step", func(p *SearchParams) { p.CRFStep = 0 }, "step"},
		{"range narrower than step", func(p *SearchParams) { p.CRFStep = 20 }, "narrower"},
		{"high bound off the grid", func(p *SearchParams) { p.CRFStep = 2 }, "whole number"},
		{"bad policy", func(p *SearchParams) { p.Policy = "nearest" }, "policy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ok
			tt.mutate(&p)
			s := &CrfSearch{logger: quiet, params: p, scoreFn: func(context.Context, float64, int) (Score, error) {
				t.Fatal("should not score")
				return Score{}, nil
			}}
			_, err := s.Run(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

// TestSearch_Simulation runs the search over many random curves and checks
// the floor contract always holds and the probe count stays low. On a 16-value
// integer grid about 3 probes is the practical floor.
func TestSearch_Simulation(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	// a slice, so every family draws the same random stream on every run
	families := []struct {
		name string
		gen  func() curve
	}{
		{"logistic", func() curve { return logistic(0.15+0.25*rng.Float64(), 24+12*rng.Float64()) }},
		{"exp", func() curve { return expDistortion(1+4*rng.Float64(), 0.12+0.12*rng.Float64(), 15+5*rng.Float64()) }},
		{"measured", func() curve { return measured(-6 + 12*rng.Float64()) }},
	}
	const runs = 400
	for _, fam := range families {
		name, gen := fam.name, fam.gen
		t.Run(name, func(t *testing.T) {
			var probes, optimal, valid int
			for range runs {
				truth := gen()
				target := 88 + 8*rng.Float64()
				initial := float64(15 + rng.IntN(16))
				p := params(target, 0.5, 15, 30, initial)
				f := newFake(t, truth, 15, 30)
				s := &CrfSearch{logger: quiet, params: p, scoreFn: f.score}
				res, err := s.Run(context.Background())
				require.NoError(t, err)
				assertStatusMatchesScore(t, p, res)
				if res.Status == StatusUnreachable || res.Status == StatusTargetExceeded {
					continue
				}
				valid++
				probes += f.coarse
				require.GreaterOrEqual(t, res.VMAF, target, "below target: %+v", res)
				if res.CRF == 30 || truth(res.CRF+1) < target {
					optimal++
				}
			}
			mean := float64(probes) / float64(valid)
			t.Logf("%s: %d searches, %.2f coarse probes mean, %.1f%% optimal", name, valid, mean, 100*float64(optimal)/float64(valid))
			assert.Less(t, mean, 3.8, "mean coarse probes")
			// early accept within tolerance can stop one CRF short of the highest
			assert.GreaterOrEqual(t, float64(optimal)/float64(valid), 0.9)
		})
	}
}

func TestOptimizedEncode_CorrectsMissOnFullEncode(t *testing.T) {
	p := params(93, 0.5, 15, 30, 20)
	var encoded []float64
	e := &OptimizedEncode{
		logger: quiet,
		params: p,
		search: func(context.Context) (SearchResult, error) {
			return SearchResult{CRF: 24, VMAF: 93.2, Status: StatusConverged}, nil
		},
		encode: func(_ context.Context, crf float64) (Score, error) {
			encoded = append(encoded, crf)
			return Score{VMAF: map[float64]float64{24: 92.7, 23: 93.6}[crf], Model: "fake"}, nil
		},
	}
	res, err := e.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []float64{24, 23}, encoded)
	assert.True(t, res.Corrected)
	assert.InDelta(t, 23, res.CRF, 0)
	assert.InDelta(t, 93.6, res.VMAF, 1e-9)
}

func TestOptimizedEncode_NoCorrection(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status Status
		policy Policy
		crf    float64
	}{
		{"unreachable", StatusUnreachable, PolicyFloor, 15},
		{"plateau", StatusPlateau, PolicyFloor, 20},
		{"closest policy", StatusConverged, PolicyClosest, 24},
		{"already at low bound", StatusBracketed, PolicyFloor, 15},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := params(93, 0.5, 15, 30, 20)
			p.Policy = tt.policy
			calls := 0
			e := &OptimizedEncode{
				logger: quiet,
				params: p,
				search: func(context.Context) (SearchResult, error) {
					return SearchResult{CRF: tt.crf, Status: tt.status}, nil
				},
				encode: func(context.Context, float64) (Score, error) {
					calls++
					return Score{VMAF: 90}, nil
				},
			}
			res, err := e.Run(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 1, calls)
			assert.False(t, res.Corrected)
		})
	}
}

func TestEncodeScorer_DropsAudio(t *testing.T) {
	s, err := NewEncodeScorer(quiet, commands.TranscodeConfig{AudioCodec: "aac"}, "/dev/null", commands.VMAFOptions{})
	require.NoError(t, err)
	defer s.Close() //nolint:errcheck
	assert.Equal(t, commands.AudioNone, s.cfg.AudioCodec)
}

func TestEncodeScorer_ClampSubsample(t *testing.T) {
	s := &EncodeScorer{refInfo: &commands.VideoInfo{VideoFrames: 192, FPS: 24, Duration: 8}}
	assert.Equal(t, 5, s.clampSubsample(5))
	assert.Equal(t, 9, s.clampSubsample(1000), "192 frames keeps at least 20 scored")
	s.refInfo = &commands.VideoInfo{VideoFrames: 3, FPS: 24}
	assert.Equal(t, 1, s.clampSubsample(5))
	s.refInfo = &commands.VideoInfo{FPS: 24, Duration: 10}
	assert.Equal(t, 5, s.clampSubsample(5))
}

// the scorer's CAMBI fallback carries into the full encode's score, so it
// doesn't fail and warn again
func TestOptimizedEncode_CarriesCambiFallback(t *testing.T) {
	e := &OptimizedEncode{
		logger: quiet,
		params: params(93, 0.5, 15, 30, 20),
		search: func(context.Context) (SearchResult, error) {
			return SearchResult{CRF: 22, Status: StatusConverged, CambiHintDropped: true, ChromaDropped: true}, nil
		},
		encode: func(context.Context, float64) (Score, error) { return Score{VMAF: 93.2}, nil },
	}
	_, err := e.Run(context.Background())
	require.NoError(t, err)
	assert.True(t, e.vmafOpts.SkipCambiHint)
	assert.True(t, e.vmafOpts.QuietChromaWarning)
}
