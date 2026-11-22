//go:build unit

package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terranvigil/dynamic-crf/actions"
	"github.com/terranvigil/dynamic-crf/commands"
)

func parse(t *testing.T, args ...string) (*options, error) {
	t.Helper()
	var out bytes.Buffer
	return parseFlags(args, &out)
}

func TestParseFlags_Defaults(t *testing.T) {
	t.Setenv("VMAF_BIN", "")
	t.Setenv("VMAF_MODEL_DIR", "")
	o, err := parse(t, "-a", "search", "-i", "in.mp4")
	require.NoError(t, err)
	require.NoError(t, o.validate())
	assert.InDelta(t, 15.0, o.search.CRFLow, 0)
	assert.InDelta(t, 30.0, o.search.CRFHigh, 0)
	assert.InDelta(t, 20.0, o.search.CRFInitial, 0)
	assert.InDelta(t, 1.0, o.search.CRFStep, 0)
	assert.InDelta(t, 95.0, o.search.TargetVMAF, 0)
	assert.Equal(t, actions.PolicyFloor, o.search.Policy)
	assert.Equal(t, actions.DefaultCoarseSubsample, o.search.CoarseSubsample)
	assert.Equal(t, commands.ModelAuto, o.vmaf.Model)
	assert.Equal(t, "copy", o.audioCodec)
	assert.Empty(t, o.deprecated)
}

func TestParseFlags_DeprecatedCRFAliases(t *testing.T) {
	// the old names were inverted: -maxcrf was max quality, the numerically low bound
	o, err := parse(t, "-a", "search", "-i", "in.mp4", "-maxcrf", "18", "-mincrf", "28")
	require.NoError(t, err)
	assert.InDelta(t, 18.0, o.search.CRFLow, 0)
	assert.InDelta(t, 28.0, o.search.CRFHigh, 0)
	assert.Len(t, o.deprecated, 2)

	o, err = parse(t, "-a", "search", "-i", "in.mp4", "-crf-low", "17.5", "-crf-high", "26", "-crfstep", "0.5", "-initialcrf", "20")
	require.NoError(t, err)
	require.NoError(t, o.validate())
	assert.InDelta(t, 17.5, o.search.CRFLow, 0)
	assert.Empty(t, o.deprecated)
}

// -h used to be an alias for -height, which hid help
func TestParseFlags_HelpIsHelp(t *testing.T) {
	_, err := parse(t, "-h")
	require.True(t, errors.Is(err, flag.ErrHelp))
}

func TestParseFlags_EnvAndHome(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	t.Setenv("VMAF_BIN", "/opt/vmaf/bin/vmaf")
	t.Setenv("VMAF_MODEL_DIR", "~/models")
	o, err := parse(t, "-a", "vmaf", "-i", "~/ref.y4m", "-o", "dist.mkv")
	require.NoError(t, err)
	assert.Equal(t, "/opt/vmaf/bin/vmaf", o.vmaf.Bin)
	assert.Equal(t, filepath.Join(home, "models"), o.vmaf.ModelDir)
	assert.Equal(t, filepath.Join(home, "ref.y4m"), o.sourcePath)

	o, err = parse(t, "-a", "vmaf", "-i", "r", "-o", "d", "-vmafbin", "/x/vmaf", "-model", "~/m/vmaf_v1.0.16_3d0h.json")
	require.NoError(t, err)
	assert.Equal(t, "/x/vmaf", o.vmaf.Bin)
	assert.Equal(t, filepath.Join(home, "m", "vmaf_v1.0.16_3d0h.json"), o.vmaf.Model)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string // empty means valid
	}{
		{"no action", []string{"-i", "in"}, "invalid action"},
		{"bad action", []string{"-a", "transcode", "-i", "in"}, "invalid action"},
		{"no input", []string{"-a", "search"}, "-i"},
		{"search with output", []string{"-a", "search", "-i", "in", "-o", "x.mp4"}, "not used"},
		{"optimize needs mp4", []string{"-a", "optimize", "-i", "in", "-o", "x.mkv"}, ".mp4"},
		{"optimize ok", []string{"-a", "optimize", "-i", "in", "-o", "x.MP4"}, ""},
		{"vmaf any extension", []string{"-a", "vmaf", "-i", "in", "-o", "dist.mkv"}, ""},
		{"cambi any extension", []string{"-a", "cambi", "-i", "in", "-o", "dist.webm"}, ""},
		{"cambi needs no input", []string{"-a", "cambi", "-o", "dist.mp4"}, ""},
		{"vmaf needs distorted", []string{"-a", "vmaf", "-i", "in"}, "distorted"},
		{"encode needs crf or bitrate", []string{"-a", "encode", "-i", "in", "-o", "x.mp4"}, "-crf or -bitrate"},
		{"encode not both", []string{"-a", "encode", "-i", "in", "-o", "x.mp4", "-crf", "23", "-bitrate", "3000"}, "both"},
		{"encode fractional crf", []string{"-a", "encode", "-i", "in", "-o", "x.mp4", "-crf", "23.5"}, ""},
		{"crf only for encode", []string{"-a", "search", "-i", "in", "-crf", "23"}, "only valid"},
		{"maxbitrate needs buffer", []string{"-a", "search", "-i", "in", "-maxbitrate", "5000"}, "-buffersize"},
		{"maxbitrate with buffer", []string{"-a", "search", "-i", "in", "-maxbitrate", "5000", "-buffersize", "10000"}, ""},
		{"bad policy", []string{"-a", "search", "-i", "in", "-policy", "nearest"}, "policy"},
		{"closest policy", []string{"-a", "search", "-i", "in", "-policy", "closest"}, ""},
		{"inverted bounds", []string{"-a", "search", "-i", "in", "-crf-low", "30", "-crf-high", "15"}, "greater than"},
		{"initial outside", []string{"-a", "search", "-i", "in", "-initialcrf", "40"}, "initial crf"},
		{"target over max", []string{"-a", "search", "-i", "in", "-targetvmaf", "120"}, "target vmaf"},
		{"zero subsample", []string{"-a", "search", "-i", "in", "-subsample", "0"}, "-subsample"},
		{"tune passes through", []string{"-a", "search", "-i", "in", "-codec", "libx265", "-tune", "fastdecode"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parse(t, tt.args...)
			require.NoError(t, err)
			err = o.validate()
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestParseFlags_RejectsStrayArgs(t *testing.T) {
	_, err := parse(t, "-a", "search", "-i", "in", "extra")
	require.ErrorContains(t, err, "unexpected")
}

func TestRun_ValidationErrorNoTools(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-a", "search"}, &stdout, &stderr)
	require.Error(t, err)
	assert.Empty(t, stdout.String())
	assert.False(t, strings.Contains(stderr.String(), "panic"))
}

func TestTranscodeConfig(t *testing.T) {
	o, err := parse(t, "-a", "encode", "-i", "in", "-o", "o.mp4", "-crf", "22.5", "-preset", "slow", "-pixfmt", "yuv420p",
		"-height", "720", "-acodec", "aac", "-abitrate", "128", "-maxbitrate", "4000", "-buffersize", "8000")
	require.NoError(t, err)
	cfg := o.transcodeConfig()
	assert.InDelta(t, 22.5, cfg.VideoCRF, 0)
	assert.Equal(t, "slow", cfg.Preset)
	assert.Equal(t, "yuv420p", cfg.PixFmt)
	assert.Equal(t, 720, cfg.Height)
	assert.Equal(t, "aac", cfg.AudioCodec)
	assert.Equal(t, 128, cfg.AudioBitrateKbps)
	assert.Equal(t, 4000, cfg.VideoMaxBitrateKbps)
}

func TestValidate_OutputChecks(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "same.mp4")
	require.NoError(t, os.WriteFile(in, []byte("x"), 0o600))

	o, err := parse(t, "-a", "optimize", "-i", in, "-o", dir+"/./same.mp4")
	require.NoError(t, err)
	require.ErrorContains(t, o.validate(), "same file")

	o, err = parse(t, "-a", "encode", "-i", in, "-o", filepath.Join(dir, "nope", "out.mp4"), "-crf", "20")
	require.NoError(t, err)
	require.ErrorContains(t, o.validate(), "does not exist")

	o, err = parse(t, "-a", "encode", "-i", in, "-o", filepath.Join(dir, "out.mp4"), "-crf", "NaN")
	require.NoError(t, err)
	require.ErrorContains(t, o.validate(), "finite")

	o, err = parse(t, "-a", "search", "-i", in, "-maxbitrate", "4000", "-buffersize", "8000")
	require.NoError(t, err)
	assert.True(t, o.search.BitrateCapped)
}

func TestValidate_DimensionsAndWritableOutput(t *testing.T) {
	o, err := parse(t, "-a", "search", "-i", "in", "-height", "-91")
	require.NoError(t, err)
	require.ErrorContains(t, o.validate(), "negative")

	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(ro, 0o555))
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	o, err = parse(t, "-a", "optimize", "-i", "in", "-o", filepath.Join(ro, "out.mp4"))
	require.NoError(t, err)
	require.ErrorContains(t, o.validate(), "not writable")
}

func TestValidate_OutputThroughSymlinkToReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(ro, 0o555))
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	link := filepath.Join(dir, "link.mp4")
	require.NoError(t, os.Symlink(filepath.Join(ro, "real.mp4"), link))
	o, err := parse(t, "-a", "optimize", "-i", "in", "-o", link)
	require.NoError(t, err)
	require.ErrorContains(t, o.validate(), "not writable")
}
