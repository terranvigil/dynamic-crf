package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// how long decoders get to exit on their own after vmaf finishes
	legExitGrace = 3 * time.Second
	// how often to nudge a vmaf still reading after both decoders exited
	eofNudgeInterval = 500 * time.Millisecond
)

// ffmpegBin is a variable so tests can stand in a fake decoder.
var ffmpegBin = "ffmpeg"

// pipeLeg is one side (reference or distorted) of a vmaf run: ffmpeg decodes
// input, applies filters and writes y4m at pixFmt into a fifo.
type pipeLeg struct {
	input   string
	filters []string
	pixFmt  string
}

func (l pipeLeg) args(fifo string) []string {
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-i", l.input, "-map", "0:V:0"}
	// vmaf's y4m reader rejects an interlaced header. scoring compares the
	// stored frames either way, so mark them progressive.
	filters := append([]string{}, l.filters...)
	filters = append(filters, "setfield=prog", "format="+l.pixFmt)
	// passthrough hands vmaf frames in decode order. letting ffmpeg's cfr
	// logic dup or drop around timestamp gaps (the joins in a scene sample)
	// makes different choices per container timebase and desyncs the legs.
	args = append(args, "-vf", joinFilters(filters), "-fps_mode", "passthrough",
		"-pix_fmt", l.pixFmt, "-strict", "-1", "-f", "yuv4mpegpipe", "-y", fifo)
	return args
}

func joinFilters(filters []string) string {
	var b bytes.Buffer
	for i, f := range filters {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(f)
	}
	return b.String()
}

type vmafFrame struct {
	Metrics map[string]float64 `json:"metrics"`
}

// chromaDroppedFrames counts frames where v1's chroma feature (SpEED) came
// out exactly zero: libvmaf couldn't estimate it, either because the frame
// has no chroma (black, gray) or the picture is too small for the model.
// models without the feature count zero.
func (j *vmafJSON) chromaDroppedFrames() int {
	n := 0
	for _, f := range j.Frames {
		for k, v := range f.Metrics {
			if strings.HasPrefix(k, "speed_chroma_uv") {
				if v == 0 {
					n++
				}
				break
			}
		}
	}
	return n
}

// vmafJSON is the subset of the vmaf CLI's --json output that we read.
type vmafJSON struct {
	// LegsWithFramesLeft names the inputs that still had frames when vmaf
	// reached the end of the other one: the two differ in length. It comes
	// from vmaf's own notice or from a decoder left writing.
	LegsWithFramesLeft []string    `json:"-"`
	Frames             []vmafFrame `json:"frames"`
	PooledMetrics      map[string]struct {
		Min          float64 `json:"min"`
		Max          float64 `json:"max"`
		Mean         float64 `json:"mean"`
		HarmonicMean float64 `json:"harmonic_mean"`
	} `json:"pooled_metrics"`
}

// runVMAFPipes runs the vmaf CLI with both inputs decoded by ffmpeg into fifos,
// so nothing scaled or converted lands on disk. vmafArgs must not include
// -r, -d, -o or --json.
//
// any process failing cancels the others: a decoder blocked opening its fifo
// would otherwise wait forever for a reader that already exited, and vmaf
// would wait forever on a fifo whose writer never started.
func runVMAFPipes(ctx context.Context, logger *slog.Logger, bin string, ref, dist pipeLeg, vmafArgs []string) (*vmafJSON, error) {
	dir, err := os.MkdirTemp("", "dcrf_vmaf_*")
	if err != nil {
		return nil, fmt.Errorf("create vmaf temp dir: %w", err)
	}
	defer os.RemoveAll(dir) //nolint:errcheck

	refFifo := filepath.Join(dir, "ref.y4m")
	distFifo := filepath.Join(dir, "dist.y4m")
	outPath := filepath.Join(dir, "out.json")
	for _, p := range []string{refFifo, distFifo} {
		if err = syscall.Mkfifo(p, 0o600); err != nil {
			return nil, fmt.Errorf("create fifo %s: %w", p, err)
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		firstErr error
		vmafDone bool
	)
	// a decoder error that happens before vmaf exits is the root cause. a
	// broken pipe never is: it means vmaf closed the fifo, either because it
	// reached the end of the shorter input or because it failed.
	fail := func(e *DecodeError) {
		mu.Lock()
		if firstErr == nil && !vmafDone && !e.BrokenPipe {
			firstErr = e
		}
		mu.Unlock()
		if !e.BrokenPipe {
			cancel()
		}
	}

	type legResult struct {
		name string
		err  *DecodeError
	}
	results := make([]legResult, 2)
	var wg sync.WaitGroup
	for li, leg := range []struct {
		name string
		leg  pipeLeg
		fifo string
	}{{"reference", ref, refFifo}, {"distorted", dist, distFifo}} {
		args := leg.leg.args(leg.fifo)
		logger.Debug("ffmpeg decode to fifo", "leg", leg.name, "args", args)
		results[li].name = leg.name
		wg.Add(1)
		go func() {
			defer wg.Done()
			var stderr bytes.Buffer
			cmd := groupCommand(runCtx, ffmpegBin, args...)
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				msg := stderr.String()
				de := &DecodeError{
					Leg:        leg.name,
					Input:      leg.leg.input,
					Err:        err,
					Stderr:     tail(msg),
					BrokenPipe: strings.Contains(msg, "Broken pipe"),
				}
				results[li].err = de
				fail(de)
			}
		}()
	}

	args := append([]string{"-r", refFifo, "-d", distFifo, "--json", "-o", outPath, "--quiet"}, vmafArgs...)
	logger.Debug("vmaf", "bin", bin, "args", args)
	var stderr, stdout bytes.Buffer
	cmd := groupCommand(runCtx, bin, args...)
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout

	legsDone := make(chan struct{})
	go func() { wg.Wait(); close(legsDone) }()
	vmafExited := make(chan struct{})
	go nudgeEOF(legsDone, vmafExited, refFifo, distFifo)

	vmafErr := cmd.Run()
	close(vmafExited)

	mu.Lock()
	vmafDone = true
	decodeErr := firstErr
	mu.Unlock()

	// after a clean vmaf exit, a decoder with frames left dies on a broken pipe
	// by itself, and one that's done is just exiting. give both a moment
	// before killing, so a finished decoder isn't killed mid-exit and its
	// "signal: killed" taken for a real failure.
	killed := false
	if vmafErr == nil {
		select {
		case <-legsDone:
		case <-time.After(legExitGrace):
			killed = true
		}
	}
	cancel()
	<-legsDone

	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case decodeErr != nil:
		return nil, decodeErr
	case vmafErr != nil:
		return nil, fmt.Errorf("%s failed: %w: %s", bin, vmafErr, tail(stderr.String()+stdout.String()))
	}

	var leftover []string
	var lateErr *DecodeError
	for _, r := range results {
		switch {
		case r.err == nil:
		case r.err.BrokenPipe:
			leftover = append(leftover, r.name)
		case killed:
			// still running after the grace period. vmaf's notice below says
			// whether it really had frames left; on its own this is just slow.
			logger.Debug("decoder slow to exit after vmaf finished", "leg", r.name)
		case lateErr == nil || isKilled(lateErr):
			// failed on its own after vmaf had read everything it was going
			// to. prefer a real failure over the other leg we killed for it.
			lateErr = r.err
		}
	}
	if lateErr != nil {
		return nil, lateErr
	}
	// vmaf reads a frame from each input per step, so one extra frame is
	// consumed and its decoder exits cleanly. vmaf still says which ended first.
	if len(leftover) == 0 {
		leftover = endedBefore(stderr.String()+stdout.String(), refFifo, distFifo)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("read vmaf output: %w", err)
	}
	out, err := parseVMAFJSON(data)
	if err != nil {
		// vmaf can exit 0 with no frames; its stderr says why, when it says anything
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, tail(msg))
		}
		return nil, err
	}
	out.LegsWithFramesLeft = leftover
	return out, nil
}

func parseVMAFJSON(data []byte) (*vmafJSON, error) {
	out := &vmafJSON{}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("parse vmaf output: %w", err)
	}
	if len(out.Frames) == 0 {
		return nil, errors.New("vmaf scored no frames")
	}
	return out, nil
}

// nudgeEOF works around a fifo close race seen on macOS (about 1 run in
// 1,500): both decoders have exited, yet vmaf stays blocked in read() and
// never sees EOF. Opening a fifo for writing without blocking and closing it
// again delivers the EOF. It does nothing until both decoders are gone, and
// a fifo with no reader just refuses the open (ENXIO).
func nudgeEOF(legsDone, vmafExited <-chan struct{}, fifos ...string) {
	select {
	case <-legsDone:
	case <-vmafExited:
		return
	}
	tick := time.NewTicker(eofNudgeInterval)
	defer tick.Stop()
	for {
		select {
		case <-vmafExited:
			return
		case <-tick.C:
			for _, p := range fifos {
				if f, err := os.OpenFile(p, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
					_ = f.Close()
				}
			}
		}
	}
}

// endedBefore reads vmaf's `"<a>" ended before "<b>".` notice and returns the
// leg that still had frames.
func endedBefore(out, refFifo, distFifo string) []string {
	// vmaf prints the raw paths in plain quotes, so build the match the same
	// way: %q would escape characters vmaf doesn't
	notice := func(a, b string) string { return `"` + a + `" ended before "` + b + `"` }
	switch {
	case strings.Contains(out, notice(refFifo, distFifo)):
		return []string{"distorted"}
	case strings.Contains(out, notice(distFifo, refFifo)):
		return []string{"reference"}
	case strings.Contains(out, "ended before"):
		return []string{"reference or distorted"}
	}
	return nil
}

func isKilled(e *DecodeError) bool {
	return strings.Contains(e.Err.Error(), "signal: killed")
}

// DecodeError is an ffmpeg leg failing to decode its input for vmaf.
type DecodeError struct {
	Leg        string
	Input      string
	Err        error
	Stderr     string
	BrokenPipe bool
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("decode %s %s failed: %v: %s", e.Leg, e.Input, e.Err, e.Stderr)
}

func (e *DecodeError) Unwrap() error { return e.Err }

// tail keeps error messages readable when ffmpeg or vmaf dump a lot of output.
func tail(s string) string {
	const limit = 1200
	if len(s) <= limit {
		return s
	}
	return "..." + s[len(s)-limit:]
}
