package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// MediaFixture returns the path of a file in fixtures/media, skipping the test
// when it's missing (see scripts/setup-test-fixtures.sh).
func MediaFixture(t *testing.T, name string) string {
	t.Helper()
	p := GetFixturePath(t, FixtureTypeMedia, name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture not found: %s (run scripts/setup-test-fixtures.sh)", p)
	}
	return p
}

// SyntheticClip writes a lossless clip of distinct test patterns, one per
// scene, each sceneSec long, so scene detection has hard cuts to find.
func SyntheticClip(t *testing.T, dir, name string, scenes int, sceneSec float64, size string) string {
	t.Helper()
	sources := []string{"testsrc2", "smptehdbars", "mandelbrot", "rgbtestsrc", "cellauto", "life"}
	args := []string{"-hide_banner", "-nostdin", "-v", "error"}
	filter := ""
	for i := range scenes {
		src := sources[i%len(sources)]
		args = append(args, "-f", "lavfi", "-t", strconv.FormatFloat(sceneSec, 'f', -1, 64), "-i", src+"=size="+size+":rate=24")
		filter += "[" + strconv.Itoa(i) + ":v]format=yuv420p,setsar=1[v" + strconv.Itoa(i) + "];"
	}
	for i := range scenes {
		filter += "[v" + strconv.Itoa(i) + "]"
	}
	filter += "concat=n=" + strconv.Itoa(scenes) + ":v=1:a=0[out]"
	out := filepath.Join(dir, name)
	args = append(args, "-filter_complex", filter, "-map", "[out]", "-c:v", "ffv1", "-y", out)
	cmdOut, err := exec.Command("ffmpeg", args...).CombinedOutput() //nolint:gosec
	require.NoError(t, err, string(cmdOut))
	return out
}
