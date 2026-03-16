//go:build unit

package actions

import (
	"context"
	"log/slog"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terranvigil/dynamic-crf/commands"
)

// simulatedVMAF returns a scoring function that models a realistic CRF→VMAF curve.
// Lower CRF = higher quality = higher VMAF. The curve has a plateau near 100 and
// drops off logarithmically, matching real-world VMAF behavior.
func simulatedVMAF(calls *int) func(ctx context.Context, samplePath string, crf int) (float64, error) {
	return func(_ context.Context, _ string, crf int) (float64, error) {
		*calls++
		// sigmoid-like model: VMAF = 100 / (1 + exp(0.3*(CRF - 28)))
		vmaf := 100.0 / (1.0 + math.Exp(0.3*(float64(crf)-28.0)))
		return vmaf, nil
	}
}

// linearVMAF is a simple linear model for baseline testing
func linearVMAF(calls *int) func(ctx context.Context, samplePath string, crf int) (float64, error) {
	return func(_ context.Context, _ string, crf int) (float64, error) {
		*calls++
		// linear: VMAF = 100 - 2*CRF (CRF 15 → 70, CRF 30 → 40)
		vmaf := 100.0 - 2.0*float64(crf)
		return vmaf, nil
	}
}

func TestCrfSearch_HybridBisection_Sigmoid(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	tests := []struct {
		name       string
		targetVMAF float64
		tolerance  float64
		crfMin     int
		crfMax     int
		crfInitial int
	}{
		{
			name:       "target 95 (high quality plateau)",
			targetVMAF: 95.0,
			tolerance:  0.5,
			crfMin:     35,
			crfMax:     10,
			crfInitial: 20,
		},
		{
			name:       "target 90 (mid-high quality)",
			targetVMAF: 90.0,
			tolerance:  2.0, // sigmoid is steep here, integer CRF can't hit exactly
			crfMin:     35,
			crfMax:     10,
			crfInitial: 20,
		},
		{
			name:       "target 80 (medium quality)",
			targetVMAF: 80.0,
			tolerance:  4.0, // steep part of sigmoid, large VMAF gap per CRF step
			crfMin:     40,
			crfMax:     10,
			crfInitial: 25,
		},
		{
			name:       "target 95 narrow range",
			targetVMAF: 95.0,
			tolerance:  2.0,
			crfMin:     25,
			crfMax:     15,
			crfInitial: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			search := &CrfSearch{
				logger:          logger,
				sourcePath:      "/dev/null",
				targetVMAF:      tt.targetVMAF,
				crfInitial:      tt.crfInitial,
				crfMin:          tt.crfMin,
				crfMax:          tt.crfMax,
				tolerance:       tt.tolerance,
				transcodeConfig: commands.TranscodeConfig{},
				scoreFn:         simulatedVMAF(&calls),
				samplePath:      "/dev/null",
			}

			crf, vmaf, err := search.Run(context.Background())
			require.NoError(t, err)
			assert.InDelta(t, tt.targetVMAF, vmaf, tt.tolerance, "VMAF should be within tolerance of target")
			assert.GreaterOrEqual(t, crf, tt.crfMax, "CRF should be >= max CRF bound")
			assert.LessOrEqual(t, crf, tt.crfMin, "CRF should be <= min CRF bound")
			assert.LessOrEqual(t, calls, maxSearchIterations, "should converge within max iterations")
			t.Logf("found CRF=%d VMAF=%.2f in %d calls", crf, vmaf, calls)
		})
	}
}

func TestCrfSearch_HybridBisection_Linear(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	var calls int
	search := &CrfSearch{
		logger:          logger,
		sourcePath:      "/dev/null",
		targetVMAF:      60.0,
		crfInitial:      20,
		crfMin:          30,
		crfMax:          15,
		tolerance:       0.5,
		transcodeConfig: commands.TranscodeConfig{},
		scoreFn:         linearVMAF(&calls),
		samplePath:      "/dev/null",
	}

	crf, vmaf, err := search.Run(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, 60.0, vmaf, 0.5)
	// linear model: VMAF=60 when CRF=20
	assert.Equal(t, 20, crf)
	t.Logf("found CRF=%d VMAF=%.2f in %d calls", crf, vmaf, calls)
}

func TestCrfSearch_Validation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	tests := []struct {
		name       string
		targetVMAF float64
		tolerance  float64
		crfMin     int
		crfMax     int
		crfInitial int
		errContain string
	}{
		{
			name:       "min crf <= max crf",
			targetVMAF: 95,
			tolerance:  0.5,
			crfMin:     10,
			crfMax:     30,
			crfInitial: 20,
			errContain: "min crf",
		},
		{
			name:       "target vmaf out of range",
			targetVMAF: 101,
			tolerance:  0.5,
			crfMin:     30,
			crfMax:     15,
			crfInitial: 20,
			errContain: "target vmaf",
		},
		{
			name:       "zero tolerance",
			targetVMAF: 95,
			tolerance:  0,
			crfMin:     30,
			crfMax:     15,
			crfInitial: 20,
			errContain: "tolerance",
		},
		{
			name:       "initial crf out of range",
			targetVMAF: 95,
			tolerance:  0.5,
			crfMin:     30,
			crfMax:     15,
			crfInitial: 5,
			errContain: "initial crf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			search := &CrfSearch{
				logger:          logger,
				sourcePath:      "/dev/null",
				targetVMAF:      tt.targetVMAF,
				crfInitial:      tt.crfInitial,
				crfMin:          tt.crfMin,
				crfMax:          tt.crfMax,
				tolerance:       tt.tolerance,
				transcodeConfig: commands.TranscodeConfig{},
				scoreFn: func(_ context.Context, _ string, _ int) (float64, error) {
					return 0, nil
				},
				samplePath: "/dev/null",
			}

			_, _, err := search.Run(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContain)
		})
	}
}

func TestCrfSearch_MaxIterations(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	var calls int
	// scoring function that never converges — always returns a score far from target
	search := &CrfSearch{
		logger:          logger,
		sourcePath:      "/dev/null",
		targetVMAF:      50.0,
		crfInitial:      20,
		crfMin:          30,
		crfMax:          15,
		tolerance:       0.001, // impossibly tight tolerance
		transcodeConfig: commands.TranscodeConfig{},
		scoreFn: func(_ context.Context, _ string, crf int) (float64, error) {
			calls++
			// returns scores that bracket the target but never hit it exactly
			return 100.0 - 3.0*float64(crf) + 0.01*float64(calls), nil
		},
		samplePath: "/dev/null",
	}

	_, _, err := search.Run(context.Background())
	require.NoError(t, err)
	// should terminate even with impossibly tight tolerance, either via max iterations or convergence
	assert.LessOrEqual(t, calls, maxSearchIterations+3, "should not exceed max iterations plus boundary evaluations")
}
