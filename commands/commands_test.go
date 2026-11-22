//go:build unit

package commands

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terranvigil/dynamic-crf/model"
)

var quiet = slog.New(slog.DiscardHandler)

func TestSelectModel(t *testing.T) {
	tests := []struct {
		model         string
		width, height int
		fps           float64
		want          string
	}{
		{"", 1920, 1080, 24, "vmaf_v1.0.16_3d0h"},
		{ModelAuto, 1280, 720, 30, "vmaf_v1.0.16_3d0h"},
		{ModelAuto, 1920, 1080, 59.94, "vmaf_v1.0.16_hfr_3d0h"},
		{ModelAuto, 2560, 1440, 24, "vmaf_v1.0.16_1d5h_2160"},
		{ModelAuto, 3840, 2160, 50, "vmaf_v1.0.16_hfr_1d5h_2160"},
		{ModelAuto, 1920, 1080, 48, "vmaf_v1.0.16_3d0h"},
		{ModelAuto, 1080, 1920, 30, "vmaf_v1.0.16_3d0h"}, // portrait 1080p
		{ModelAuto, 2160, 3840, 30, "vmaf_v1.0.16_1d5h_2160"},
		{"vmaf_v1.0.16_5d0h", 3840, 2160, 60, "vmaf_v1.0.16_5d0h"},
		{"vmaf_v0.6.1", 1920, 1080, 24, "vmaf_v0.6.1"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, SelectModel(tt.model, tt.width, tt.height, tt.fps), "%+v", tt)
	}
}

func TestModelMaxScore(t *testing.T) {
	assert.InDelta(t, 110.0, ModelMaxScore("vmaf_v1.0.16_3d0h_2160"), 0)
	assert.InDelta(t, 110.0, ModelMaxScore("/m/vmaf_v1.0.16_hfr_3d0h_2160.json"), 0)
	assert.InDelta(t, 100.0, ModelMaxScore("vmaf_v1.0.16_1d5h_2160"), 0)
	assert.InDelta(t, 100.0, ModelMaxScore("vmaf_v1.0.16_3d0h"), 0)
	assert.InDelta(t, 100.0, ModelMaxScore("vmaf_v0.6.1"), 0)
}

func TestModelParam(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "vmaf_v1.0.16_hfr"), 0o755))
	flat := filepath.Join(dir, "vmaf_v1.0.16_3d0h.json")
	nested := filepath.Join(dir, "vmaf_v1.0.16_hfr", "vmaf_v1.0.16_hfr_3d0h.json")
	for _, p := range []string{flat, nested} {
		require.NoError(t, os.WriteFile(p, []byte("{}"), 0o600))
	}

	got, err := modelParam("vmaf_v1.0.16_3d0h", "")
	require.NoError(t, err)
	assert.Equal(t, "version=vmaf_v1.0.16_3d0h", got)

	got, err = modelParam("vmaf_v1.0.16_3d0h", dir)
	require.NoError(t, err)
	assert.Equal(t, "path="+flat, got)

	got, err = modelParam("vmaf_v1.0.16_hfr_3d0h", dir)
	require.NoError(t, err)
	assert.Equal(t, "path="+nested, got)

	got, err = modelParam(flat, "")
	require.NoError(t, err)
	assert.Equal(t, "path="+flat, got)

	_, err = modelParam("vmaf_v1.0.16_5d0h", dir)
	require.ErrorContains(t, err, "not found")

	_, err = modelParam(filepath.Join(dir, "missing.json"), "")
	require.Error(t, err)

	_, err = modelParam("vmaf_v1.0.16_3d0h", "/x/mo:del")
	require.ErrorContains(t, err, "can't contain ':'")
	_, err = modelParam("/x/a:b.json", "")
	require.ErrorContains(t, err, "can't contain ':'")
}

func TestVMAFArgs(t *testing.T) {
	dist := &VideoInfo{Width: 1280, Height: 720, BitDepth: 8}

	args, err := vmafArgs(VMAFOptions{Threads: 3, Subsample: 5}, "vmaf_v1.0.16_3d0h", dist, true)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"--threads", "3",
		"--model", "version=vmaf_v1.0.16_3d0h:name=vmaf:cambi.enc_width=1280:cambi.enc_height=720:cambi.enc_bitdepth=8",
		"--subsample", "5",
	}, args)

	// the fallback when libvmaf rejects the encode size
	args, err = vmafArgs(VMAFOptions{Threads: 3}, "vmaf_v1.0.16_3d0h", dist, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"--threads", "3", "--model", "version=vmaf_v1.0.16_3d0h:name=vmaf"}, args)

	// v0 models have no cambi feature to configure; subsample 1 is the default
	args, err = vmafArgs(VMAFOptions{Threads: 2, Subsample: 1}, "vmaf_v0.6.1", dist, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"--threads", "2", "--model", "version=vmaf_v0.6.1:name=vmaf"}, args)
}

func TestScoreLegs(t *testing.T) {
	ref := &VideoInfo{Width: 1920, Height: 1080, FPS: 24, FPSRatio: "24/1"}

	r, d := scoreLegs("ref.y4m", "dist.mp4", ref, &VideoInfo{Width: 1920, Height: 1080, FPS: 24})
	assert.Empty(t, r.filters)
	assert.Empty(t, d.filters)
	assert.Equal(t, scorePixFmt, r.pixFmt)
	assert.Equal(t, scorePixFmt, d.pixFmt)

	_, d = scoreLegs("ref.y4m", "dist.mp4", ref, &VideoInfo{Width: 1280, Height: 720, FPS: 30})
	assert.Equal(t, []string{"scale=1920:1080:flags=bicubic", "fps=24/1"}, d.filters)

	// a variable-rate side is never resampled: its encode matches it frame for frame
	_, d = scoreLegs("ref.mkv", "dist.mp4", &VideoInfo{Width: 640, Height: 360, FPS: 30.57, FPSRatio: "4800/157", VFR: true},
		&VideoInfo{Width: 640, Height: 360, FPS: 21.3, VFR: true})
	assert.Empty(t, d.filters)
}

func TestPipeLegArgs(t *testing.T) {
	leg := pipeLeg{input: "in.mp4", filters: []string{"scale=1920:1080:flags=bicubic"}, pixFmt: "yuv420p10le"}
	assert.Equal(t, []string{
		"-hide_banner", "-nostdin", "-v", "error", "-i", "in.mp4", "-map", "0:V:0",
		"-vf", "scale=1920:1080:flags=bicubic,setfield=prog,format=yuv420p10le", "-fps_mode", "passthrough",
		"-pix_fmt", "yuv420p10le", "-strict", "-1", "-f", "yuv4mpegpipe", "-y", "/tmp/f",
	}, leg.args("/tmp/f"))
}

func TestBitDepth(t *testing.T) {
	assert.Equal(t, 8, bitDepth("yuv420p", ""))
	assert.Equal(t, 8, bitDepth("yuv410p", ""))
	assert.Equal(t, 10, bitDepth("yuv420p10le", ""))
	assert.Equal(t, 10, bitDepth("p010le", ""))
	assert.Equal(t, 12, bitDepth("yuv444p12be", ""))
	assert.Equal(t, 10, bitDepth("yuv420p", "10"))
}

func TestVideoInfoFromMetadata(t *testing.T) {
	meta := &model.FfprobeMetadata{
		Streams: []model.FfprobeStream{
			{CodecType: "audio"},
			{CodecType: "video", Width: 1920, Height: 1080, RFrameRate: "24000/1001", PixFmt: "yuv420p10le"},
		},
		Format: model.FfprobeFormat{Duration: "120.000", Size: "15000000"},
	}
	info, err := VideoInfoFromMetadata(meta, "x.mp4")
	require.NoError(t, err)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 10, info.BitDepth)
	assert.InDelta(t, 23.976, info.FPS, 0.001)
	assert.InDelta(t, 120.0, info.Duration, 0)
	assert.Equal(t, 1000, info.BitrateKbps())

	_, err = VideoInfoFromMetadata(&model.FfprobeMetadata{}, "x.mp4")
	require.ErrorContains(t, err, "no video stream")
}

func TestVideoInfoFromMetadata_Rotation(t *testing.T) {
	for _, st := range []model.FfprobeStream{
		{CodecType: "video", Width: 640, Height: 360, RFrameRate: "24/1", SideDataList: []model.FfprobeSideData{{SideDataType: "Display Matrix", Rotation: -90}}},
		{CodecType: "video", Width: 640, Height: 360, RFrameRate: "24/1", Tags: model.FfprobeStreamTag{Rotate: "270"}},
	} {
		info, err := VideoInfoFromMetadata(&model.FfprobeMetadata{Streams: []model.FfprobeStream{st}}, "rot.mp4")
		require.NoError(t, err)
		assert.Equal(t, 360, info.Width, "rotated sources decode at their displayed size")
		assert.Equal(t, 640, info.Height)
	}
	st := model.FfprobeStream{
		CodecType: "video", Width: 640, Height: 360, RFrameRate: "24/1",
		SideDataList: []model.FfprobeSideData{{Rotation: 180}},
	}
	info, err := VideoInfoFromMetadata(&model.FfprobeMetadata{Streams: []model.FfprobeStream{st}}, "flip.mp4")
	require.NoError(t, err)
	assert.Equal(t, 640, info.Width)
}

func TestVideoInfoFromMetadata_VariableRate(t *testing.T) {
	meta := &model.FfprobeMetadata{
		Streams: []model.FfprobeStream{{CodecType: "video", Width: 1280, Height: 720, RFrameRate: "60/1", AvgFrameRate: "30000/1001"}},
	}
	info, err := VideoInfoFromMetadata(meta, "vfr.mp4")
	require.NoError(t, err)
	assert.InDelta(t, 29.97, info.FPS, 0.01, "variable rate should use the average, not r_frame_rate")
	assert.Equal(t, "30000/1001", info.FPSRatio)
	assert.Equal(t, "vmaf_v1.0.16_3d0h", SelectModel(ModelAuto, info.Width, info.Height, info.FPS))

	meta.Streams[0].AvgFrameRate = "16624/695" // a scene sample: 23.92, close enough to 24
	meta.Streams[0].RFrameRate = "24/1"
	info, err = VideoInfoFromMetadata(meta, "sample.mkv")
	require.NoError(t, err)
	assert.Equal(t, "24/1", info.FPSRatio)
}

func TestVideoFrames(t *testing.T) {
	assert.Equal(t, 144, videoFrames(&model.FfprobeStream{NbFrames: "144"}, 24))
	assert.Equal(t, 144, videoFrames(&model.FfprobeStream{Duration: "6.000"}, 24))
	assert.Equal(t, 1441, videoFrames(&model.FfprobeStream{Tags: model.FfprobeStreamTag{Duration: "00:01:00.041000000"}}, 24))
	assert.Equal(t, 0, videoFrames(&model.FfprobeStream{}, 24))
}

func TestEndedBefore(t *testing.T) {
	assert.Equal(t, []string{"distorted"}, endedBefore(`"/t/ref.y4m" ended before "/t/dist.y4m".`, "/t/ref.y4m", "/t/dist.y4m"))
	assert.Equal(t, []string{"reference"}, endedBefore(`"/t/dist.y4m" ended before "/t/ref.y4m".`, "/t/ref.y4m", "/t/dist.y4m"))
	assert.Empty(t, endedBefore("", "/t/ref.y4m", "/t/dist.y4m"))
	// characters Go's %q would escape
	odd := "/t/a\u00a0\"b"
	assert.Equal(t, []string{"distorted"}, endedBefore(`"`+odd+`/ref.y4m" ended before "`+odd+`/dist.y4m".`, odd+"/ref.y4m", odd+"/dist.y4m"))
}

func TestCheckModelSize(t *testing.T) {
	require.NoError(t, CheckModelSize("vmaf_v1.0.16_3d0h", 320, 176))
	require.NoError(t, CheckModelSize("vmaf_v1.0.16_3d0h", 160, 240))
	require.NoError(t, CheckModelSize("vmaf_v1.0.16_3d0h", 7680, 4320))
	require.NoError(t, CheckModelSize("vmaf_v1.0.16_1d5h_2160", 7680, 4400), "the cambi retry handles past 7680 wide")
	require.NoError(t, CheckModelSize("vmaf_v1.0.16_1d5h_2160", 8192, 3456))
	require.ErrorContains(t, CheckModelSize("vmaf_v1.0.16_1d5h_2160", 8192, 4320), "at most 12092")
	require.ErrorContains(t, CheckModelSize("vmaf_v1.0.16_1d5h_2160", 6100, 6100), "at most 12092")
	require.ErrorContains(t, CheckModelSize("vmaf_v1.0.16_3d0h", 256, 144), "vmaf_v0.6.1")
	require.ErrorContains(t, CheckModelSize("vmaf_v1.0.16_3d0h", 200, 200), "vmaf_v0.6.1")
	require.NoError(t, CheckModelSize("vmaf_v0.6.1", 160, 90))
	// farther-viewing models need more picture
	require.ErrorContains(t, CheckModelSize("vmaf_v1.0.16_5d0h", 426, 240), "short side of at least 270")
	require.NoError(t, CheckModelSize("vmaf_v1.0.16_hfr_5d0h", 480, 270))
	require.ErrorContains(t, CheckModelSize("vmaf_v1.0.16_3d0h_2160", 480, 300), "at least 320")
	require.NoError(t, CheckModelSize("/m/vmaf_v1.0.16_3d0h_2160.json", 480, 320))
}

func TestChromaDroppedFrames(t *testing.T) {
	out, err := parseVMAFJSON([]byte(`{"frames":[
		{"metrics":{"speed_chroma_uv_mxv_45":0,"v1":90}},
		{"metrics":{"speed_chroma_uv_mxv_45":3.2,"v1":91}},
		{"metrics":{"speed_chroma_uv_mxv_45":0,"v1":92}},
		{"metrics":{"vmaf":95}}],"pooled_metrics":{}}`))
	require.NoError(t, err)
	assert.Equal(t, 2, out.chromaDroppedFrames())
}

func TestParseVMAFJSON(t *testing.T) {
	out, err := parseVMAFJSON([]byte(`{"frames":[{"frameNum":0},{"frameNum":1}],
		"pooled_metrics":{"vmaf":{"min":90.1,"max":96.2,"mean":93.4,"harmonic_mean":93.3}}}`))
	require.NoError(t, err)
	assert.Len(t, out.Frames, 2)
	assert.InDelta(t, 93.4, out.PooledMetrics["vmaf"].Mean, 1e-9)

	_, err = parseVMAFJSON([]byte(`{"frames":[],"pooled_metrics":{}}`))
	require.ErrorContains(t, err, "no frames")

	_, err = parseVMAFJSON([]byte(`not json`))
	require.Error(t, err)
}

func TestEscapeFilterValue(t *testing.T) {
	// the example from ffmpeg's filtergraph escaping notes
	in := `this is a 'string': may contain one, or more, special characters`
	want := `this is a \\\'string\\\'\\: may contain one\, or more\, special characters`
	assert.Equal(t, want, escapeFilterValue(in))
	assert.Equal(t, `/plain/path.mp4`, escapeFilterValue(`/plain/path.mp4`))
	assert.Equal(t, `a\[1\]\;b\\\\c`, escapeFilterValue(`a[1];b\c`))
}

func TestEncodeArgs(t *testing.T) {
	base := TranscodeConfig{VideoCodec: "libx264", VideoCRF: 23.5}
	tests := []struct {
		name    string
		mutate  func(c *TranscodeConfig)
		want    []string
		notWant []string
	}{
		{"fractional crf, no audio", func(*TranscodeConfig) {}, []string{"-crf 23.5", "-an", "-map 0:V:0", "-fps_mode passthrough"}, []string{"-b:v", "-map 0:a?"}},
		{"output rate replaces passthrough", func(c *TranscodeConfig) { c.FPSNumerator, c.FPSDenominator = 30, 1 }, []string{"-r 30/1"}, []string{"-fps_mode"}},
		{"integer crf", func(c *TranscodeConfig) { c.VideoCRF = 23 }, []string{"-crf 23 "}, nil},
		{"vp9 constant quality", func(c *TranscodeConfig) { c.VideoCodec = "libvpx-vp9" }, []string{"-crf 23.5 -b:v 0"}, nil},
		{"aom constant quality", func(c *TranscodeConfig) { c.VideoCodec = "libaom-av1" }, []string{"-b:v 0"}, nil},
		{"svt-av1 needs no zero bitrate", func(c *TranscodeConfig) { c.VideoCodec = "libsvtav1" }, nil, []string{"-b:v"}},
		{"audio copy", func(c *TranscodeConfig) { c.AudioCodec = "copy" }, []string{"-map 0:a?", "-c:a copy"}, []string{"-an"}},
		{"audio none", func(c *TranscodeConfig) { c.AudioCodec = AudioNone }, []string{"-an"}, []string{"-c:a"}},
		{"aac bitrate", func(c *TranscodeConfig) { c.AudioCodec, c.AudioBitrateKbps = "aac", 128 }, []string{"-c:a aac -b:a 128k"}, nil},
		{
			"preset tune pixfmt", func(c *TranscodeConfig) { c.Preset, c.Tune, c.PixFmt = "slow", "film", "yuv420p" },
			[]string{"-preset slow", "-tune film", "-pix_fmt yuv420p"},
			nil,
		},
		{"height only keeps aspect", func(c *TranscodeConfig) { c.Height = 720 }, []string{"-vf scale=-2:720:flags=lanczos"}, nil},
		{"vbv", func(c *TranscodeConfig) { c.VideoMaxBitrateKbps, c.VideoBufferSizeKbps = 5000, 10000 }, []string{"-maxrate 5000k", "-bufsize 10000k"}, nil},
		{"bitrate mode", func(c *TranscodeConfig) { c.VideoCRF, c.VideoBitrateKbps = 0, 3000 }, []string{"-b:v 3000k"}, []string{"-crf"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.mutate(&cfg)
			args, err := NewFfmpegEncode(quiet, "in.y4m", "out.mp4", cfg).args()
			require.NoError(t, err)
			joined := strings.Join(args, " ") + " "
			for _, w := range tt.want {
				assert.Contains(t, joined, w)
			}
			for _, nw := range tt.notWant {
				assert.NotContains(t, joined, nw)
			}
			assert.Equal(t, "out.mp4", args[len(args)-1])
		})
	}

	_, err := NewFfmpegEncode(quiet, "in", "out.mp4", TranscodeConfig{VideoCodec: "libx264"}).args()
	require.ErrorContains(t, err, "bitrate or crf")
}

func TestFormatCRF(t *testing.T) {
	assert.Equal(t, "23", FormatCRF(23))
	assert.Equal(t, "23.5", FormatCRF(23.5))
	assert.Equal(t, "23.25", FormatCRF(23.25))
}

func TestSceneArgs(t *testing.T) {
	args := sceneArgs("src.mov", "out.mkv", model.Scene{StartPTSSec: 12.3456, Duration: 4})
	joined := strings.Join(args, " ")
	// -ss before -i seeks frame-accurately when transcoding
	assert.Less(t, strings.Index(joined, "-ss 12.345600"), strings.Index(joined, "-i src.mov"))
	assert.Contains(t, joined, "-t 4.000000")
	assert.Contains(t, joined, "setpts=PTS-STARTPTS")
	assert.Contains(t, joined, "-c:v ffv1")
	assert.Contains(t, joined, "-g 1")

	assert.Equal(t, `'/tmp/a'\''b.mkv'`, concatQuote(`/tmp/a'b.mkv`))
}

func TestCambiPixFmt(t *testing.T) {
	assert.Equal(t, "yuv420p", cambiPixFmt(8))
	assert.Equal(t, "yuv420p10le", cambiPixFmt(10))
}

func TestVMAFOptionsDefaults(t *testing.T) {
	t.Setenv("VMAF_BIN", "/opt/vmaf")
	assert.Equal(t, "/opt/vmaf", VMAFOptions{}.bin())
	assert.Equal(t, "/x/vmaf", VMAFOptions{Bin: "/x/vmaf"}.bin())
	t.Setenv("VMAF_BIN", "")
	assert.Equal(t, "vmaf", VMAFOptions{}.bin())
	assert.GreaterOrEqual(t, VMAFOptions{}.threads(), 1)
	assert.Equal(t, 7, VMAFOptions{}.WithSubsample(7).Subsample)
}

// fake tools for runVMAFPipes: a decoder that writes to its last argument and
// a vmaf that reads both fifos, each controllable by script body.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700)) //nolint:gosec
	return p
}

const (
	// writes a little data to the fifo named by the last argument
	decoderOK = `for a; do last=$a; done; printf 'YUV4MPEG2 W2 H2\n' > "$last"`
	// exits without ever opening its fifo
	decoderFail = `echo "decode boom" >&2; exit 1`
	// reads -r and -d fifos then writes json to -o
	vmafOK = `while [ $# -gt 0 ]; do case $1 in -r) r=$2;; -d) d=$2;; -o) o=$2;; esac; shift; done
cat "$r" > /dev/null; cat "$d" > /dev/null
printf '{"frames":[{"frameNum":0}],"pooled_metrics":{"vmaf":{"mean":94.5}}}' > "$o"`
	// writes until the reader goes away, then fails the way ffmpeg does
	decoderLong = `for a; do last=$a; done
yes > "$last" 2>/dev/null || { echo "av_interleaved_write_frame(): Broken pipe" >&2; exit 1; }`
	// reads a few bytes of each input, then succeeds
	vmafReadsLittle = `while [ $# -gt 0 ]; do case $1 in -r) r=$2;; -d) d=$2;; -o) o=$2;; esac; shift; done
exec 3<"$r" 4<"$d"
head -c 64 <&3 > /dev/null; head -c 64 <&4 > /dev/null
printf '{"frames":[{"frameNum":0}],"pooled_metrics":{"vmaf":{"mean":94.5}}}' > "$o"`
	// fails before opening either fifo
	vmafFail = `echo "model load failed" >&2; exit 2`
)

func withFakeFfmpeg(t *testing.T, path string) {
	t.Helper()
	old := ffmpegBin
	ffmpegBin = path
	t.Cleanup(func() { ffmpegBin = old })
}

func runPipesWithTimeout(t *testing.T, bin string) (*vmafJSON, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	leg := pipeLeg{input: "x", pixFmt: "yuv420p"}
	out, err := runVMAFPipes(ctx, quiet, bin, leg, leg, nil)
	require.NoError(t, ctx.Err(), "runVMAFPipes hung")
	return out, err
}

func TestRunVMAFPipes(t *testing.T) {
	dir := t.TempDir()

	t.Run("success", func(t *testing.T) {
		withFakeFfmpeg(t, writeScript(t, dir, "ff_ok", decoderOK))
		out, err := runPipesWithTimeout(t, writeScript(t, dir, "vmaf_ok", vmafOK))
		require.NoError(t, err)
		assert.InDelta(t, 94.5, out.PooledMetrics["vmaf"].Mean, 1e-9)
		assert.Empty(t, out.LegsWithFramesLeft)
	})

	// the old cambi code hung here: decoders blocked opening fifos nobody reads
	t.Run("vmaf fails before reading", func(t *testing.T) {
		withFakeFfmpeg(t, writeScript(t, dir, "ff_ok2", decoderOK))
		_, err := runPipesWithTimeout(t, writeScript(t, dir, "vmaf_fail", vmafFail))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model load failed")
	})

	// and the mirror: vmaf blocked opening a fifo whose writer died
	t.Run("decoder fails before writing", func(t *testing.T) {
		withFakeFfmpeg(t, writeScript(t, dir, "ff_fail", decoderFail))
		_, err := runPipesWithTimeout(t, writeScript(t, dir, "vmaf_ok2", vmafOK))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode boom")
	})

	// vmaf stops at the shorter input and exits 0; the other decoder then dies
	// on a broken pipe. that used to race and throw the good score away.
	t.Run("broken pipe after success is not an error", func(t *testing.T) {
		withFakeFfmpeg(t, writeScript(t, dir, "ff_long", decoderLong))
		bin := writeScript(t, dir, "vmaf_short", vmafReadsLittle)
		for range 25 {
			out, err := runPipesWithTimeout(t, bin)
			require.NoError(t, err)
			assert.InDelta(t, 94.5, out.PooledMetrics["vmaf"].Mean, 1e-9)
			// but it does mean the inputs differ in length
			assert.ElementsMatch(t, []string{"reference", "distorted"}, out.LegsWithFramesLeft)
		}
	})

	// a decoder that wrote everything and is merely slow to exit isn't one
	// with frames left
	t.Run("finished decoder slow to exit", func(t *testing.T) {
		withFakeFfmpeg(t, writeScript(t, dir, "ff_slow_exit", decoderOK+"; sleep 0.5"))
		out, err := runPipesWithTimeout(t, writeScript(t, dir, "vmaf_ok5", vmafOK))
		require.NoError(t, err)
		assert.Empty(t, out.LegsWithFramesLeft)
	})

	// a decoder that fails after vmaf finished reading isn't fallout
	t.Run("decoder error after vmaf succeeds", func(t *testing.T) {
		withFakeFfmpeg(t, writeScript(t, dir, "ff_late_fail", decoderOK+`; sleep 0.3; echo "Invalid data found" >&2; exit 69`))
		_, err := runPipesWithTimeout(t, writeScript(t, dir, "vmaf_ok6", vmafOK))
		require.ErrorContains(t, err, "Invalid data found")
	})

	// when one leg fails after vmaf and we kill the other for it, the real
	// failure is what's reported, whichever leg it was
	t.Run("late real failure beats a killed leg", func(t *testing.T) {
		withFakeFfmpeg(t, writeScript(t, dir, "ff_mixed", decoderOK+`
case "$last" in
*ref.y4m) sleep 10 ;;
*) sleep 0.3; echo "dist leg broke" >&2; exit 69 ;;
esac`))
		_, err := runPipesWithTimeout(t, writeScript(t, dir, "vmaf_ok7", vmafOK))
		require.ErrorContains(t, err, "dist leg broke")
	})

	// vmaf left waiting on a fifo after both decoders exited, as in the macOS
	// close race: the reopen below blocks until something opens the fifo for
	// writing, which only the EOF nudge does
	t.Run("vmaf stuck after decoders exit", func(t *testing.T) {
		withFakeFfmpeg(t, writeScript(t, dir, "ff_ok_n", decoderOK))
		stuck := writeScript(t, dir, "vmaf_stuck", `while [ $# -gt 0 ]; do case $1 in -r) r=$2;; -d) d=$2;; -o) o=$2;; esac; shift; done
cat "$r" > /dev/null; cat "$d" > /dev/null
sleep 0.2
cat "$r" > /dev/null
printf '{"frames":[{"frameNum":0}],"pooled_metrics":{"vmaf":{"mean":94.5}}}' > "$o"`)
		out, err := runPipesWithTimeout(t, stuck)
		require.NoError(t, err)
		assert.InDelta(t, 94.5, out.PooledMetrics["vmaf"].Mean, 1e-9)
	})

	t.Run("cancelled context", func(t *testing.T) {
		withFakeFfmpeg(t, writeScript(t, dir, "ff_ok4", decoderOK))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		leg := pipeLeg{input: "x", pixFmt: "yuv420p"}
		_, err := runVMAFPipes(ctx, quiet, writeScript(t, dir, "vmaf_ok3", vmafOK), leg, leg, nil)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("missing vmaf binary", func(t *testing.T) {
		withFakeFfmpeg(t, writeScript(t, dir, "ff_ok3", decoderOK))
		_, err := runPipesWithTimeout(t, filepath.Join(dir, "does-not-exist"))
		require.Error(t, err)
	})
}

func TestCheckDistinct(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	require.NoError(t, os.WriteFile(in, []byte("x"), 0o600))
	link := filepath.Join(dir, "link.mp4")
	require.NoError(t, os.Link(in, link))

	// ffmpeg's own guard compares strings and misses all of these
	require.Error(t, checkDistinct(in, dir+"/./in.mp4"))
	require.Error(t, checkDistinct(in, link))
	require.NoError(t, checkDistinct(in, filepath.Join(dir, "out.mp4")))
	require.NoError(t, checkDistinct(filepath.Join(dir, "missing"), in))
}

func TestSafeOutPath(t *testing.T) {
	assert.Equal(t, "./-dash.mp4", safeOutPath("-dash.mp4"))
	assert.Equal(t, "/abs/-dash.mp4", safeOutPath("/abs/-dash.mp4"))
	assert.Equal(t, "out.mp4", safeOutPath("out.mp4"))
}

// a failed encode must not leave a partial file or clobber an existing target
func TestFfmpegEncode_FailureLeavesTargetAlone(t *testing.T) {
	dir := t.TempDir()
	withFakeFfmpeg(t, writeScript(t, dir, "ff_encode_fail", `for a; do last=$a; done; echo junk > "$last"; exit 1`))
	target := filepath.Join(dir, "out.mp4")
	require.NoError(t, os.WriteFile(target, []byte("previous"), 0o600))

	err := NewFfmpegEncode(quiet, filepath.Join(dir, "in.y4m"), target, TranscodeConfig{VideoCodec: "libx264", VideoCRF: 20}).Run(context.Background())
	require.Error(t, err)
	got, _ := os.ReadFile(target)
	assert.Equal(t, "previous", string(got))
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".partial-")
	}
}

// two runs encoding to one -o used to share a partial file and corrupt it
func TestFfmpegEncode_ConcurrentSameTarget(t *testing.T) {
	dir := t.TempDir()
	withFakeFfmpeg(t, writeScript(t, dir, "ff_slow", `for a; do last=$a; done; printf "complete" > "$last"; sleep 0.3`))
	target := filepath.Join(dir, "out.mp4")
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			errs <- NewFfmpegEncode(quiet, filepath.Join(dir, "in.y4m"), target, TranscodeConfig{VideoCodec: "libx264", VideoCRF: 20}).Run(context.Background())
		}()
	}
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "complete", string(got))
}

func TestFfmpegEncode_WritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	withFakeFfmpeg(t, writeScript(t, dir, "ff_write", `for a; do last=$a; done; printf "new" > "$last"`))
	real := filepath.Join(dir, "real.mp4")
	link := filepath.Join(dir, "link.mp4")
	require.NoError(t, os.WriteFile(real, []byte("old"), 0o600))
	require.NoError(t, os.Symlink(real, link))

	require.NoError(t, NewFfmpegEncode(quiet, filepath.Join(dir, "in.y4m"), link, TranscodeConfig{VideoCodec: "libx264", VideoCRF: 20}).Run(context.Background()))
	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "link should stay a link")
	got, _ := os.ReadFile(real)
	assert.Equal(t, "new", string(got))
}

func TestVideoStream_SkipsCoverArt(t *testing.T) {
	meta := &model.FfprobeMetadata{Streams: []model.FfprobeStream{
		{Index: 0, CodecType: "video", Width: 600, Height: 600, Disposition: model.FfprobeDispostion{AttachedPic: 1}},
		{Index: 1, CodecType: "video", Width: 1920, Height: 1080, RFrameRate: "24/1"},
	}}
	require.Equal(t, 1, meta.VideoStream().Index)
	info, err := VideoInfoFromMetadata(meta, "cover.mkv")
	require.NoError(t, err)
	assert.Equal(t, 1920, info.Width)
}

// partials used to come from os.CreateTemp (0600), and the rename carried that
// mode to every output
func TestFfmpegEncode_OutputModeFollowsUmask(t *testing.T) {
	dir := t.TempDir()
	withFakeFfmpeg(t, writeScript(t, dir, "ff_mode", `for a; do last=$a; done; printf "x" > "$last"`))
	target := filepath.Join(dir, "out.mp4")
	require.NoError(t, NewFfmpegEncode(quiet, filepath.Join(dir, "in.y4m"), target, TranscodeConfig{VideoCodec: "libx264", VideoCRF: 20}).Run(context.Background()))

	mask := syscall.Umask(0)
	syscall.Umask(mask)
	fi, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o666&^mask), fi.Mode().Perm())
}

func TestResolveOutput_DanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link.mp4")
	require.NoError(t, os.Symlink("sub/real.mp4", link))
	resolve := func(p string) string {
		t.Helper()
		r, err := ResolveOutput(p)
		require.NoError(t, err)
		return r
	}
	assert.Equal(t, filepath.Join(dir, "sub", "real.mp4"), resolve(link))
	assert.Equal(t, filepath.Join(dir, "plain.mp4"), resolve(filepath.Join(dir, "plain.mp4")))
	chain := filepath.Join(dir, "chain.mp4")
	require.NoError(t, os.Symlink("link.mp4", chain))
	assert.Equal(t, filepath.Join(dir, "sub", "real.mp4"), resolve(chain), "chains of dangling links are followed")
	require.NoError(t, os.Symlink("loopb.mp4", filepath.Join(dir, "loopa.mp4")))
	require.NoError(t, os.Symlink("loopa.mp4", filepath.Join(dir, "loopb.mp4")))
	_, err := ResolveOutput(filepath.Join(dir, "loopa.mp4"))
	require.ErrorContains(t, err, "loop")

	withFakeFfmpeg(t, writeScript(t, dir, "ff_dangle", `for a; do last=$a; done; printf "x" > "$last"`))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, NewFfmpegEncode(quiet, filepath.Join(dir, "in.y4m"), link, TranscodeConfig{VideoCodec: "libx264", VideoCRF: 20}).Run(context.Background()))
	fi, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode()&os.ModeSymlink, "the dangling link is written through, not replaced")
	_, err = os.Stat(filepath.Join(dir, "sub", "real.mp4"))
	require.NoError(t, err)
}

// an mp4 edit list (a stream-copy trim) leaves nb_frames counting hidden frames
func TestVideoFrames_EditList(t *testing.T) {
	assert.Equal(t, 144, videoFrames(&model.FfprobeStream{NbFrames: "288", Duration: "6.000"}, 24))
	assert.Equal(t, 143, videoFrames(&model.FfprobeStream{NbFrames: "143", Duration: "6.000"}, 24))
}
