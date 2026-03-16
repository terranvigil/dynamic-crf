//go:build integration

package actions

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terranvigil/dynamic-crf/commands"
)

func fixtureDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "fixtures", "media")
}

func shortFixture() string {
	return filepath.Join(fixtureDir(), "sintel_trailer_1080p.y4m")
}

func longFixture() string {
	return filepath.Join(fixtureDir(), "big_buck_bunny_120s_1080p.mp4")
}

func skipIfNoFixture(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s (run scripts/setup-test-fixtures.sh)", path)
	}
}

// TestCrfSearch_Integration_ShortClip tests the full CRF search pipeline
// against the Sintel trailer (~52s, under the 60s scene sampling threshold).
// Validates that the returned CRF achieves a VMAF within tolerance of the target.
func TestCrfSearch_Integration_ShortClip(t *testing.T) {
	source := shortFixture()
	skipIfNoFixture(t, source)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	tests := []struct {
		name       string
		targetVMAF float64
		tolerance  float64
		codec      string
		height     int // 0 = native resolution
	}{
		{
			name:       "target 95 native resolution",
			targetVMAF: 95.0,
			tolerance:  0.5,
			codec:      "libx264",
		},
		{
			name:       "target 93 native resolution",
			targetVMAF: 93.0,
			tolerance:  0.5,
			codec:      "libx264",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := commands.TranscodeConfig{
				VideoCodec: tt.codec,
				Height:     tt.height,
			}

			search := NewCrfSearch(logger, source, tt.targetVMAF, 20, 30, 15, tt.tolerance, cfg)
			crf, vmaf, err := search.Run(context.Background())
			require.NoError(t, err)

			t.Logf("target=%.1f found CRF=%d VMAF=%.2f", tt.targetVMAF, crf, vmaf)

			assert.GreaterOrEqual(t, crf, 15, "CRF should be within search bounds")
			assert.LessOrEqual(t, crf, 30, "CRF should be within search bounds")

			// verify the result by doing a full-accuracy VMAF score
			verifyScore := verifyVMAF(t, logger, source, crf, cfg)
			t.Logf("verification VMAF=%.2f (search reported %.2f)", verifyScore, vmaf)

			// the verified score should be close to what the search reported
			// allow some delta because search uses subsampled scoring
			assert.InDelta(t, vmaf, verifyScore, 2.0, "search VMAF should be close to full-accuracy VMAF")

			// the verified score should be reasonably close to the target
			// integer CRF granularity means we can't always hit within tolerance
			assert.InDelta(t, tt.targetVMAF, verifyScore, 3.0,
				fmt.Sprintf("verified VMAF %.2f should be near target %.1f", verifyScore, tt.targetVMAF))
		})
	}
}

// TestCrfSearch_Integration_SceneSampling tests the scene sampling path
// using Big Buck Bunny (~10min), which is well over the 60s threshold
// and triggers scene detection and sample creation.
func TestCrfSearch_Integration_SceneSampling(t *testing.T) {
	source := longFixture()
	skipIfNoFixture(t, source)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := commands.TranscodeConfig{
		VideoCodec: "libx264",
	}

	search := NewCrfSearch(logger, source, 95.0, 20, 30, 15, 0.5, cfg)
	crf, vmaf, err := search.Run(context.Background())
	require.NoError(t, err)

	t.Logf("found CRF=%d VMAF=%.2f (via scene sampling)", crf, vmaf)

	assert.GreaterOrEqual(t, crf, 15)
	assert.LessOrEqual(t, crf, 30)

	// verify against the full source at full accuracy
	// the sample-based search should generalize well to the full video
	verifyScore := verifyVMAF(t, logger, source, crf, cfg)
	t.Logf("full-source verification VMAF=%.2f (sample-based search reported %.2f)", verifyScore, vmaf)

	assert.InDelta(t, 95.0, verifyScore, 4.0,
		"full-source VMAF should be near target even though search used a scene sample")
}

// TestCrfSearch_Integration_Efficiency verifies the hybrid bisection search
// uses fewer scoring iterations than a naive linear scan would require.
func TestCrfSearch_Integration_Efficiency(t *testing.T) {
	source := shortFixture()
	skipIfNoFixture(t, source)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cfg := commands.TranscodeConfig{
		VideoCodec: "libx264",
	}

	var calls int
	search := NewCrfSearch(logger, source, 95.0, 20, 30, 15, 0.5, cfg)

	// wrap the real scoring function to count calls
	originalScoreFn := search.getScoreFn()
	search.scoreFn = func(ctx context.Context, samplePath string, crf int) (float64, error) {
		calls++
		return originalScoreFn(ctx, samplePath, crf)
	}

	crf, vmaf, err := search.Run(context.Background())
	require.NoError(t, err)

	t.Logf("found CRF=%d VMAF=%.2f in %d scoring calls", crf, vmaf, calls)

	// CRF range is 15-30 = 16 possible values
	// linear scan would need up to 16 calls
	// hybrid bisection should need far fewer
	assert.LessOrEqual(t, calls, 8,
		fmt.Sprintf("hybrid search used %d calls, should be significantly fewer than linear scan of 16", calls))
}

// TestOptimizedEncode_Integration tests the full optimize pipeline:
// search for CRF, encode full video, and VMAF-score the result.
func TestOptimizedEncode_Integration(t *testing.T) {
	source := shortFixture()
	skipIfNoFixture(t, source)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	target, err := os.CreateTemp("", "optimize_test_*.mp4")
	require.NoError(t, err)
	defer os.Remove(target.Name()) //nolint:errcheck

	cfg := commands.TranscodeConfig{
		VideoCodec: "libx264",
	}

	opt := NewOptimizedEncoded(logger, cfg, source, target.Name(), 95.0, 20, 30, 15, 0.5)
	err = opt.Run(context.Background())
	require.NoError(t, err)

	// verify output file exists and is non-empty
	info, err := os.Stat(target.Name())
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0), "output file should not be empty")

	// verify VMAF of the actual output
	score, err := commands.NewFfmpegVMAF(logger, target.Name(), source, 1).Run(context.Background())
	require.NoError(t, err)
	t.Logf("optimized encode VMAF=%.2f", score)

	assert.Greater(t, score, 90.0, "optimized encode should achieve high VMAF")
}

// verifyVMAF encodes at a given CRF and scores with full accuracy (speed=1)
func verifyVMAF(t *testing.T, logger *slog.Logger, source string, crf int, cfg commands.TranscodeConfig) float64 {
	t.Helper()

	tmpEncode, err := os.CreateTemp("", "verify_*.mp4")
	require.NoError(t, err)
	defer os.Remove(tmpEncode.Name()) //nolint:errcheck

	verifyCfg := cfg
	verifyCfg.VideoCRF = crf
	err = commands.NewFfmpegEncode(logger, source, tmpEncode.Name(), verifyCfg).Run(context.Background())
	require.NoError(t, err)

	// speed=1 for highest accuracy verification
	score, err := commands.NewFfmpegVMAF(logger, tmpEncode.Name(), source, 1).Run(context.Background())
	require.NoError(t, err)

	return score
}
