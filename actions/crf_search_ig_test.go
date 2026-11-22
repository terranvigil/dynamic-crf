//go:build integration

package actions

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/testutil"
)

var igLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

// sintel trailer, about 52s of 1080p24 y4m: under the sampling threshold
const shortFixture = "sintel_trailer_1080p.y4m"

// big buck bunny, 120s of near-lossless 1080p24 mp4: sampled
const longFixture = "big_buck_bunny_120s_1080p.mp4"

func requireVMAFv1(t *testing.T) commands.VMAFOptions {
	t.Helper()
	opts := commands.VMAFOptions{ModelDir: os.Getenv("VMAF_MODEL_DIR")}
	if err := commands.CheckVMAF(context.Background(), igLogger, opts); err != nil {
		t.Skipf("vmaf binary can't score VMAF v1, set VMAF_BIN: %v", err)
	}
	return opts
}

func igParams(target float64) SearchParams {
	return SearchParams{
		TargetVMAF:      target,
		Tolerance:       0.5,
		CRFLow:          15,
		CRFHigh:         30,
		CRFInitial:      20,
		CRFStep:         1,
		Policy:          PolicyFloor,
		CoarseSubsample: DefaultCoarseSubsample,
	}
}

var igConfig = commands.TranscodeConfig{VideoCodec: "libx264", Preset: "veryfast"}

// exactScore encodes source at crf and scores every frame against it.
func exactScore(t *testing.T, source string, crf float64, opts commands.VMAFOptions) float64 {
	t.Helper()
	s, err := NewEncodeScorer(igLogger, igConfig, source, opts)
	require.NoError(t, err)
	defer s.Close() //nolint:errcheck
	res, err := s.Score(context.Background(), crf, 1)
	require.NoError(t, err)
	return res.VMAF
}

// TestCrfSearch_Integration_Synthetic runs the whole search, scene sampling
// included, on a generated clip, so it works without the media fixtures.
func TestCrfSearch_Integration_Synthetic(t *testing.T) {
	opts := requireVMAFv1(t)
	src := testutil.SyntheticClip(t, t.TempDir(), "src.mkv", 4, 2, "640x360")

	for _, sample := range []bool{false, true} {
		name := "full source"
		if sample {
			name = "scene sample"
		}
		t.Run(name, func(t *testing.T) {
			s := NewCrfSearch(igLogger, src, igParams(93), igConfig, opts)
			if sample {
				s.sceneSampleMinDurSec = 1
			}
			res, err := s.Run(context.Background())
			require.NoError(t, err)
			t.Logf("%+v", res)
			assert.Equal(t, "vmaf_v1.0.16_3d0h", res.Model)
			assert.True(t, res.Exact)
			if res.Status != StatusTargetExceeded && res.Status != StatusUnreachable {
				assert.GreaterOrEqual(t, res.VMAF, 93.0)
			}
			assert.LessOrEqual(t, res.Encodes, 6)
		})
	}
}

// TestCrfSearch_Integration_ShortClip searches the full Sintel trailer and
// checks the floor contract against independent encodes.
func TestCrfSearch_Integration_ShortClip(t *testing.T) {
	opts := requireVMAFv1(t)
	source := testutil.MediaFixture(t, shortFixture)

	for _, target := range []float64{93, 95} {
		t.Run(commands.FormatCRF(target), func(t *testing.T) {
			res, err := NewCrfSearch(igLogger, source, igParams(target), igConfig, opts).Run(context.Background())
			require.NoError(t, err)
			t.Logf("%+v", res)
			require.Contains(t, []Status{StatusConverged, StatusBracketed}, res.Status)
			assert.GreaterOrEqual(t, res.VMAF, target)
			assert.LessOrEqual(t, res.Scores, 7, "coarse probes plus exact re-scores")

			if res.CRF < 30 {
				next := exactScore(t, source, res.CRF+1, opts)
				t.Logf("crf %g scores %.2f", res.CRF+1, next)
				if res.Status == StatusBracketed {
					assert.Less(t, next, target, "crf %g should be the highest meeting the target", res.CRF)
				}
			}
		})
	}
}

// TestCrfSearch_Integration_SceneSampling searches Big Buck Bunny on a scene
// sample and checks the result carries over to the full two minutes.
func TestCrfSearch_Integration_SceneSampling(t *testing.T) {
	opts := requireVMAFv1(t)
	source := testutil.MediaFixture(t, longFixture)

	res, err := NewCrfSearch(igLogger, source, igParams(95), igConfig, opts).Run(context.Background())
	require.NoError(t, err)
	t.Logf("%+v", res)
	assert.GreaterOrEqual(t, res.VMAF, 95.0)

	full := exactScore(t, source, res.CRF, opts)
	t.Logf("full-source vmaf %.2f at crf %g (sample said %.2f)", full, res.CRF, res.VMAF)
	assert.InDelta(t, res.VMAF, full, 2.0, "sample should generalize to the full source")
}

func TestOptimizedEncode_Integration(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 3, 2, "640x360")
	out := filepath.Join(dir, "out.mp4")

	cfg := igConfig
	cfg.AudioCodec = "copy"
	res, err := NewOptimizedEncode(igLogger, cfg, src, out, igParams(93), opts).Run(context.Background())
	require.NoError(t, err)
	t.Logf("%+v", res)

	info, err := os.Stat(out)
	require.NoError(t, err)
	assert.Positive(t, info.Size())
	assert.Equal(t, out, res.Output)
	if res.Search.Status != StatusTargetExceeded && res.Search.Status != StatusUnreachable {
		assert.GreaterOrEqual(t, res.VMAF, 93.0)
	}
}

// MPEG-TS starts its clock at 1.4s or later. scene times used to come out
// absolute while -ss is relative, so every scene started late and the last
// one seeked past the end.
func TestCrfSearch_Integration_TSStartTime(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 4, 2.5, "640x360")
	ts := filepath.Join(dir, "src.ts")
	enc := commands.TranscodeConfig{VideoCodec: "libx264", VideoCRF: 4, Preset: "veryfast"}
	require.NoError(t, commands.NewFfmpegEncode(igLogger, src, filepath.Join(dir, "tmp.mp4"), enc).Run(context.Background()))
	out, err := exec.Command("ffmpeg", "-v", "error", "-i", filepath.Join(dir, "tmp.mp4"), "-c", "copy", "-y", ts).CombinedOutput()
	require.NoError(t, err, string(out))

	s := NewCrfSearch(igLogger, ts, igParams(93), igConfig, opts)
	s.sceneSampleMinDurSec = 1
	res, err := s.Run(context.Background())
	require.NoError(t, err)
	t.Logf("%+v", res)
	assert.Greater(t, res.VMAF, 80.0, "a desynced sample scores far lower")
}

// a rotated source decodes at its displayed size; the encode used to be
// scored against the coded size and fail on mismatched dimensions
func TestCrfSearch_Integration_RotatedSource(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 1, 2, "640x360")
	rot := filepath.Join(dir, "rot.mp4")
	enc := commands.TranscodeConfig{VideoCodec: "libx264", VideoCRF: 4, Preset: "veryfast"}
	require.NoError(t, commands.NewFfmpegEncode(igLogger, src, filepath.Join(dir, "tmp.mp4"), enc).Run(context.Background()))
	out, err := exec.Command("ffmpeg", "-v", "error", "-display_rotation", "90", "-i", filepath.Join(dir, "tmp.mp4"), "-c", "copy", "-y", rot).CombinedOutput()
	require.NoError(t, err, string(out))

	scorer, err := NewEncodeScorer(igLogger, igConfig, rot, opts)
	require.NoError(t, err)
	defer scorer.Close() //nolint:errcheck
	sc, err := scorer.Score(context.Background(), 20, 1)
	require.NoError(t, err)
	assert.Greater(t, sc.VMAF, 80.0)
}

// a variable-rate source and its encode can probe to different average rates;
// resampling one side used to desync the legs and score in the teens
func TestCrfSearch_Integration_VariableFrameRate(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	vfr := filepath.Join(dir, "vfr.mkv")
	out, err := exec.Command("ffmpeg", "-v", "error",
		"-f", "lavfi", "-t", "2", "-i", "testsrc2=size=640x360:rate=24",
		"-f", "lavfi", "-t", "2", "-i", "mandelbrot=size=640x360:rate=60",
		"-filter_complex", "[0:v]format=yuv420p,setsar=1[a];[1:v]format=yuv420p,setsar=1[b];[a][b]concat=n=2:v=1[v]",
		"-map", "[v]", "-fps_mode", "vfr", "-c:v", "ffv1", "-y", vfr).CombinedOutput()
	require.NoError(t, err, string(out))

	res, err := NewCrfSearch(igLogger, vfr, igParams(93), igConfig, opts).Run(context.Background())
	require.NoError(t, err)
	t.Logf("%+v", res)
	assert.Greater(t, res.VMAF, 80.0)
}

// container duration includes audio; a source whose audio outlasts its video
// used to fail every score on the frame-count check
func TestCrfSearch_Integration_AudioLongerThanVideo(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "long_audio.mp4")
	out, err := exec.Command("ffmpeg", "-v", "error",
		"-f", "lavfi", "-t", "3", "-i", "testsrc2=size=640x360:rate=24",
		"-f", "lavfi", "-t", "5", "-i", "sine=frequency=440",
		"-c:v", "libx264", "-crf", "4", "-preset", "veryfast", "-c:a", "aac", "-y", src).CombinedOutput()
	require.NoError(t, err, string(out))

	res, err := NewCrfSearch(igLogger, src, igParams(93), igConfig, opts).Run(context.Background())
	require.NoError(t, err)
	t.Logf("%+v", res)
}

// a stream-copy trim leaves an mp4 edit list: nb_frames counts frames the
// decoder never shows, and the old frame check failed every score
func TestCrfSearch_Integration_EditListTrim(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	full := filepath.Join(dir, "full.mp4")
	trim := filepath.Join(dir, "trim.mp4")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-t", "6", "-i", "testsrc2=size=640x360:rate=24",
		"-c:v", "libx264", "-crf", "4", "-preset", "veryfast", "-g", "240", "-y", full).CombinedOutput()
	require.NoError(t, err, string(out))
	out, err = exec.Command("ffmpeg", "-v", "error", "-ss", "3", "-i", full, "-c", "copy", "-y", trim).CombinedOutput()
	require.NoError(t, err, string(out))

	res, err := NewCrfSearch(igLogger, trim, igParams(93), igConfig, opts).Run(context.Background())
	require.NoError(t, err)
	t.Logf("%+v", res)
}

// libvmaf rejects CAMBI encode sizes far below the scoring size; a 144p rung
// of a 360p source used to be unscoreable
func TestEncodeScorer_Integration_SmallRung(t *testing.T) {
	opts := requireVMAFv1(t)
	src := testutil.SyntheticClip(t, t.TempDir(), "src.mkv", 1, 2, "640x360")
	cfg := igConfig
	cfg.Height = 144
	s, err := NewEncodeScorer(igLogger, cfg, src, opts)
	require.NoError(t, err)
	defer s.Close() //nolint:errcheck
	sc, err := s.Score(context.Background(), 23, 1)
	require.NoError(t, err)
	assert.Positive(t, sc.VMAF)
}

// a long variable-rate source goes through the scene sampler, whose FFV1
// output probes as constant rate; the old frame estimate failed every score
func TestCrfSearch_Integration_LongVariableFrameRate(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "vfr_long.mkv")
	var args []string
	var filter string
	const parts = 14
	for i := range parts {
		rate := []string{"30", "24"}[i%2]
		args = append(args, "-f", "lavfi", "-t", "5", "-i", []string{"testsrc2", "mandelbrot"}[i%2]+"=size=320x240:rate="+rate)
		filter += "[" + strconv.Itoa(i) + ":v]format=yuv420p,setsar=1[v" + strconv.Itoa(i) + "];"
	}
	for i := range parts {
		filter += "[v" + strconv.Itoa(i) + "]"
	}
	filter += "concat=n=" + strconv.Itoa(parts) + ":v=1[out]"
	args = append([]string{"-v", "error"}, args...)
	args = append(args, "-filter_complex", filter, "-map", "[out]", "-fps_mode", "vfr", "-c:v", "libx264", "-crf", "4", "-preset", "ultrafast", "-y", src)
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	require.NoError(t, err, string(out))

	res, err := NewCrfSearch(igLogger, src, igParams(90), igConfig, opts).Run(context.Background())
	require.NoError(t, err)
	t.Logf("%+v", res)
	assert.Greater(t, res.VMAF, 70.0)
}
