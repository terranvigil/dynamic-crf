//go:build integration

package commands

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terranvigil/dynamic-crf/model"
	"github.com/terranvigil/dynamic-crf/testutil"
)

var igLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

// requireVMAFv1 skips unless $VMAF_BIN (or vmaf on PATH) can score with v1.
func requireVMAFv1(t *testing.T) VMAFOptions {
	t.Helper()
	opts := VMAFOptions{ModelDir: os.Getenv("VMAF_MODEL_DIR")}
	if err := CheckVMAF(context.Background(), igLogger, opts); err != nil {
		t.Skipf("vmaf binary can't score VMAF v1, set VMAF_BIN: %v", err)
	}
	return opts
}

func encodeAt(t *testing.T, src, dir string, crf float64, height int) string {
	t.Helper()
	out := filepath.Join(dir, "crf_"+FormatCRF(crf)+".mp4")
	cfg := TranscodeConfig{VideoCodec: "libx264", VideoCRF: crf, Preset: "veryfast", Height: height}
	require.NoError(t, NewFfmpegEncode(igLogger, src, out, cfg).Run(context.Background()))
	return out
}

func TestVMAF_V1_Integration(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 2, 2, "1280x720")

	ctx := context.Background()
	hi, err := NewVMAF(igLogger, encodeAt(t, src, dir, 18, 0), src, opts).Run(ctx)
	require.NoError(t, err)
	lo, err := NewVMAF(igLogger, encodeAt(t, src, dir, 40, 0), src, opts).Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, "vmaf_v1.0.16_3d0h", hi.Model)
	assert.InDelta(t, 100.0, hi.MaxScore, 0)
	assert.Equal(t, 96, hi.Frames)
	assert.Greater(t, hi.Score, lo.Score, "crf 18 should outscore crf 40")
	assert.Greater(t, hi.Score, 80.0)
	t.Logf("crf18=%.2f crf40=%.2f", hi.Score, lo.Score)

	// subsampling scores fewer frames
	sub, err := NewVMAF(igLogger, encodeAt(t, src, dir, 18, 0), src, opts.WithSubsample(5)).Run(ctx)
	require.NoError(t, err)
	assert.Less(t, sub.Frames, hi.Frames)
	assert.InDelta(t, hi.Score, sub.Score, 2)
}

// a downscaled rung is scaled back up to the reference for scoring
func TestVMAF_V1_Integration_ScaledRung(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 1, 2, "1280x720")
	rung := encodeAt(t, src, dir, 20, 360)

	res, err := NewVMAF(igLogger, rung, src, opts).Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 640, res.Distorted.Width)
	assert.Equal(t, 48, res.Frames)
	assert.Greater(t, res.Score, 0.0)
}

func TestCambi_Integration(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 1, 1, "640x360")
	res, err := NewCambi(igLogger, encodeAt(t, src, dir, 30, 0), opts).Run(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, res.Mean, 0.0)
	assert.GreaterOrEqual(t, res.Max, res.Mean)
}

// the old movie= string broke on commas, quotes, brackets and colons
func TestFfprobeScenes_Integration_SpecialCharacterPath(t *testing.T) {
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "a, b'c [x];d:e.mkv", 3, 1, "320x180")
	frames, err := NewFfprobeScenes(igLogger, src, 0).Run(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, frames.Frames, "should find the cuts between patterns")
}

// the old sampler stream-copied into .ts, which failed for codecs like FFV1,
// ProRes and raw y4m, and started each scene at the next keyframe
func TestFfmpegSampler_Integration(t *testing.T) {
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 3, 2, "320x180")
	out := filepath.Join(dir, "sample"+SampleExt)
	scenes := []model.Scene{{StartPTSSec: 0.5, Duration: 1}, {StartPTSSec: 2.25, Duration: 1.5}, {StartPTSSec: 4, Duration: 2}}
	require.NoError(t, NewFfmpegSampler(igLogger, src, out, scenes).Run(context.Background()))

	info, err := ProbeVideo(context.Background(), igLogger, out)
	require.NoError(t, err)
	assert.Equal(t, 320, info.Width)
	assert.InDelta(t, 4.5, info.Duration, 0.1, "24+36+48 frames at 24fps")
}

// the joins in a scene sample leave timestamp gaps. scoring an mp4 encode of
// the sample used to desync the legs at each join and lose 10 to 30 points.
func TestVMAF_V1_Integration_SampleStaysInSync(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 3, 3, "640x360")
	sample := filepath.Join(dir, "sample"+SampleExt)
	scenes := []model.Scene{{StartPTSSec: 0.3, Duration: 2}, {StartPTSSec: 3.1, Duration: 2.5}, {StartPTSSec: 6.2, Duration: 2.4}}
	require.NoError(t, NewFfmpegSampler(igLogger, src, sample, scenes).Run(context.Background()))

	res, err := NewVMAF(igLogger, encodeAt(t, sample, dir, 16, 0), sample, opts).Run(context.Background())
	require.NoError(t, err)
	t.Logf("sample vmaf at crf 16: %.2f over %d frames", res.Score, res.Frames)
	assert.Greater(t, res.Score, 90.0)
}

// vmaf's y4m reader rejected the interlaced header ffmpeg writes for
// interlaced-flagged sources
func TestVMAF_V1_Integration_Interlaced(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "inter.ts")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-t", "2", "-i", "testsrc2=size=720x480:rate=30",
		"-vf", "setfield=tff", "-c:v", "libx264", "-flags", "+ildct+ilme", "-crf", "4", "-y", src).CombinedOutput()
	require.NoError(t, err, string(out))
	res, err := NewVMAF(igLogger, encodeAt(t, src, dir, 20, 0), src, opts).Run(context.Background())
	require.NoError(t, err)
	assert.Greater(t, res.Score, 80.0)
}

func TestCheckAudioMux_Integration(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "mulaw.mkv")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-t", "1", "-i", "testsrc2=size=320x180:rate=24",
		"-f", "lavfi", "-t", "1", "-i", "sine", "-c:v", "ffv1", "-c:a", "pcm_mulaw", "-y", src).CombinedOutput()
	require.NoError(t, err, string(out))

	ctx := context.Background()
	require.ErrorContains(t, CheckAudioMux(ctx, igLogger, src, TranscodeConfig{AudioCodec: "copy"}), "-acodec")
	require.NoError(t, CheckAudioMux(ctx, igLogger, src, TranscodeConfig{AudioCodec: "aac"}))
	require.NoError(t, CheckAudioMux(ctx, igLogger, src, TranscodeConfig{AudioCodec: AudioNone}))

	silent := testutil.SyntheticClip(t, dir, "silent.mkv", 1, 1, "320x180")
	require.NoError(t, CheckAudioMux(ctx, igLogger, silent, TranscodeConfig{AudioCodec: "copy"}), "no audio, nothing to check")
}

// vmaf consumes one extra frame from the longer input, so a one-frame
// mismatch leaves no decoder writing; vmaf's own notice still catches it
func TestVMAF_V1_Integration_OneFrameShort(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 1, 2, "320x180")
	short := filepath.Join(dir, "short.mp4")
	out, err := exec.Command("ffmpeg", "-v", "error", "-i", src, "-frames:v", "47", "-c:v", "libx264", "-crf", "20", "-y", short).CombinedOutput()
	require.NoError(t, err, string(out))

	res, err := NewVMAF(igLogger, short, src, opts).Run(context.Background())
	require.NoError(t, err, "lenient scoring warns")
	assert.Equal(t, 47, res.Frames)

	strict := opts
	strict.StrictFrames = true
	_, err = NewVMAF(igLogger, short, src, strict).Run(context.Background())
	require.ErrorContains(t, err, "differ in length")
}

// v1 can't score tiny pictures (libvmaf fails or crashes); say so plainly
func TestVMAF_V1_Integration_TooSmall(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 1, 1, "160x90")
	_, err := NewVMAF(igLogger, encodeAt(t, src, dir, 20, 0), src, opts).Run(context.Background())
	require.ErrorContains(t, err, "vmaf_v0.6.1")

	// just above the floor scores, including a small portrait picture whose
	// CAMBI hint libvmaf refuses
	for _, size := range []string{"320x176", "160x240"} {
		small := testutil.SyntheticClip(t, dir, "small_"+size+".mkv", 1, 1, size)
		res, err := NewVMAF(igLogger, encodeAt(t, small, dir, 20, 0), small, opts).Run(context.Background())
		require.NoError(t, err, size)
		assert.Positive(t, res.Score, size)
	}

	v0 := opts
	v0.Model = "vmaf_v0.6.1"
	res, err := NewVMAF(igLogger, encodeAt(t, src, dir, 20, 0), src, v0).Run(context.Background())
	require.NoError(t, err)
	assert.Positive(t, res.Score)
}

// farther-viewing v1 models need a bigger picture; libvmaf crashes below it
func TestVMAF_V1_Integration_PerModelFloor(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 1, 1, "426x240")
	enc := encodeAt(t, src, dir, 20, 0)

	phone := opts
	phone.Model = "vmaf_v1.0.16_5d0h"
	_, err := NewVMAF(igLogger, enc, src, phone).Run(context.Background())
	require.ErrorContains(t, err, "short side of at least 270")

	// the default model scores it, with chroma dropped for want of pixels
	res, err := NewVMAF(igLogger, enc, src, opts).Run(context.Background())
	require.NoError(t, err)
	assert.Positive(t, res.Score)

	_, err = NewVMAF(igLogger, enc, src, VMAFOptions{Model: "vmaf_v1.0.16_bogus"}).Run(context.Background())
	require.ErrorContains(t, err, "no built-in model")
}

// libvmaf flags flat-chroma frames (a fade to black) the same way as a
// picture too small for v1's chroma feature; only the latter drops chroma
func TestVMAF_V1_Integration_FadeIsNotTooSmall(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "fade.mkv")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24,fade=t=out:st=0:d=0.4",
		"-frames:v", "12", "-c:v", "ffv1", "-y", src).CombinedOutput()
	require.NoError(t, err, string(out))
	res, err := NewVMAF(igLogger, encodeAt(t, src, dir, 20, 0), src, opts).Run(context.Background())
	require.NoError(t, err)
	assert.False(t, res.ChromaDropped)
}

// the size at which v1's chroma feature drops out depends on the model
func TestVMAF_V1_Integration_ChromaDroppedPerModel(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := testutil.SyntheticClip(t, dir, "src.mkv", 1, 1, "720x480")
	enc := encodeAt(t, src, dir, 20, 0)

	res, err := NewVMAF(igLogger, enc, src, opts).Run(context.Background())
	require.NoError(t, err)
	assert.False(t, res.ChromaDropped, "3d0h has enough pixels at 720x480")

	phone := opts
	phone.Model = "vmaf_v1.0.16_5d0h"
	res, err = NewVMAF(igLogger, enc, src, phone).Run(context.Background())
	require.NoError(t, err)
	assert.True(t, res.ChromaDropped, "5d0h needs more than 720x480")
}

// libvmaf prints several notices per flat frame; a clip with a third of its
// frames black used to be flagged as having dropped chroma throughout
func TestVMAF_V1_Integration_BlackStretchKeepsChroma(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "black_intro.mkv")
	out, err := exec.Command("ffmpeg", "-v", "error",
		"-f", "lavfi", "-t", "1", "-i", "color=black:size=1280x720:rate=24",
		"-f", "lavfi", "-t", "2", "-i", "testsrc2=size=1280x720:rate=24",
		"-filter_complex", "[0:v]format=yuv420p[a];[1:v]format=yuv420p[b];[a][b]concat=n=2:v=1[v]",
		"-map", "[v]", "-c:v", "ffv1", "-y", src).CombinedOutput()
	require.NoError(t, err, string(out))
	res, err := NewVMAF(igLogger, encodeAt(t, src, dir, 20, 0), src, opts).Run(context.Background())
	require.NoError(t, err)
	assert.False(t, res.ChromaDropped)
}

func TestCambi_Integration_SizeLimits(t *testing.T) {
	opts := requireVMAFv1(t)
	dir := t.TempDir()
	tiny := testutil.SyntheticClip(t, dir, "tiny.mkv", 1, 1, "192x108")
	_, err := NewCambi(igLogger, tiny, opts).Run(context.Background())
	require.ErrorContains(t, err, "long side of at least 216")
}
