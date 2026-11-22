package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/terranvigil/dynamic-crf/actions"
	"github.com/terranvigil/dynamic-crf/commands"
)

const (
	actionOptimize = "optimize"
	actionSearch   = "search"
	actionEncode   = "encode"
	actionInspect  = "inspect"
	actionVmaf     = "vmaf"
	actionCambi    = "cambi"

	defaultTolerance  = 0.5
	defaultCRFLow     = 15
	defaultCRFHigh    = 30
	defaultCRFInitial = 20
	defaultCRFStep    = 1
	defaultTargetVMAF = 95.0
)

var validActions = []string{actionOptimize, actionSearch, actionEncode, actionInspect, actionVmaf, actionCambi}

type options struct {
	action     string
	sourcePath string
	targetPath string
	jsonOut    bool
	verbose    bool

	crf         float64
	bitrateKbps int

	search actions.SearchParams

	codec          string
	preset         string
	tune           string
	pixFmt         string
	width, height  int
	maxBitrateKbps int
	minBitrateKbps int
	bufferSizeKbps int
	audioCodec     string
	audioKbps      int

	vmaf commands.VMAFOptions

	// deprecated flags that were set, for a warning
	deprecated []string
}

func (o *options) transcodeConfig() commands.TranscodeConfig {
	return commands.TranscodeConfig{
		VideoCodec:          o.codec,
		Preset:              o.preset,
		Tune:                o.tune,
		PixFmt:              o.pixFmt,
		Width:               o.width,
		Height:              o.height,
		VideoCRF:            o.crf,
		VideoBitrateKbps:    o.bitrateKbps,
		VideoMinBitrateKbps: o.minBitrateKbps,
		VideoMaxBitrateKbps: o.maxBitrateKbps,
		VideoBufferSizeKbps: o.bufferSizeKbps,
		AudioCodec:          o.audioCodec,
		AudioBitrateKbps:    o.audioKbps,
	}
}

// parseFlags parses args (without the program name). Usage goes to out.
func parseFlags(args []string, out io.Writer) (*options, error) {
	o := &options{}
	fs := flag.NewFlagSet("dynamic-crf", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(out, "usage: dynamic-crf -a <%s> -i <input> [options]\n\n", strings.Join(validActions, "|"))
		fs.PrintDefaults()
	}

	actionHelp := "action: " + strings.Join(validActions, ", ")
	fs.StringVar(&o.action, "action", "", actionHelp)
	fs.StringVar(&o.action, "a", "", actionHelp)
	fs.StringVar(&o.sourcePath, "input", "", "input (reference) file; not used by cambi")
	fs.StringVar(&o.sourcePath, "i", "", "input (reference) file; not used by cambi")
	fs.StringVar(&o.targetPath, "output", "", "output file for optimize/encode (.mp4), or the distorted file for vmaf/cambi")
	fs.StringVar(&o.targetPath, "o", "", "output file for optimize/encode (.mp4), or the distorted file for vmaf/cambi")
	fs.BoolVar(&o.jsonOut, "json", false, "print the result as JSON on stdout")
	fs.BoolVar(&o.verbose, "verbose", false, "debug logging, including every ffmpeg and vmaf command")

	fs.Float64Var(&o.crf, "crf", 0, "crf for the encode action")
	fs.IntVar(&o.bitrateKbps, "bitrate", 0, "bitrate in kbps for the encode action")

	p := &o.search
	fs.Float64Var(&p.TargetVMAF, "targetvmaf", defaultTargetVMAF, "target VMAF score")
	fs.Float64Var(&p.Tolerance, "tolerance", defaultTolerance, "acceptance window in VMAF points above the target (either side with -policy closest)")
	fs.Float64Var(&p.CRFLow, "crf-low", defaultCRFLow, "lowest crf searched (highest quality, largest file)")
	fs.Float64Var(&p.CRFHigh, "crf-high", defaultCRFHigh, "highest crf searched (lowest quality, smallest file)")
	fs.Float64Var(&p.CRFLow, "maxcrf", defaultCRFLow, "deprecated: use -crf-low")
	fs.Float64Var(&p.CRFHigh, "mincrf", defaultCRFHigh, "deprecated: use -crf-high")
	fs.Float64Var(&p.CRFInitial, "initialcrf", defaultCRFInitial, "first crf tried")
	fs.Float64Var(&p.CRFStep, "crfstep", defaultCRFStep, "crf grid step; 0.5 or 0.25 work with libx264 and libx265")
	policy := fs.String("policy", string(actions.PolicyFloor), "floor: highest crf at or above target; closest: crf nearest the target either side")

	fs.StringVar(&o.codec, "codec", "libx264", "video encoder")
	fs.StringVar(&o.preset, "preset", "", "encoder preset; the crf found only holds for the preset it was searched with")
	fs.StringVar(&o.tune, "tune", "", "encoder tune, passed through to ffmpeg")
	fs.StringVar(&o.tune, "t", "", "encoder tune, passed through to ffmpeg")
	fs.StringVar(&o.pixFmt, "pixfmt", "", "output pixel format, e.g. yuv420p; empty leaves it to the encoder")
	fs.IntVar(&o.width, "width", 0, "output width in pixels (aspect ratio kept if height is unset)")
	fs.IntVar(&o.height, "height", 0, "output height in pixels (aspect ratio kept if width is unset)")
	fs.IntVar(&o.maxBitrateKbps, "maxbitrate", 0, "peak bitrate cap in kbps; needs -buffersize")
	fs.IntVar(&o.maxBitrateKbps, "mb", 0, "peak bitrate cap in kbps; needs -buffersize")
	fs.IntVar(&o.bufferSizeKbps, "buffersize", 0, "VBV buffer size in kbits")
	fs.IntVar(&o.bufferSizeKbps, "bs", 0, "VBV buffer size in kbits")
	fs.IntVar(&o.minBitrateKbps, "minbitrate", 0, "minimum bitrate in kbps")
	fs.StringVar(&o.audioCodec, "acodec", "copy", "audio for optimize/encode output: an ffmpeg encoder, copy, or none")
	fs.IntVar(&o.audioKbps, "abitrate", 0, "audio bitrate in kbps when -acodec is an encoder")

	v := &o.vmaf
	fs.StringVar(&v.Model, "model", commands.ModelAuto, "VMAF model: auto (v1, picked by height and frame rate), a model name, or a .json path")
	fs.StringVar(&v.Bin, "vmafbin", os.Getenv("VMAF_BIN"), "vmaf executable built from libvmaf 3.2.1+ (default $VMAF_BIN, then vmaf on PATH)")
	fs.StringVar(&v.ModelDir, "vmafmodeldir", os.Getenv("VMAF_MODEL_DIR"), "directory of VMAF model JSON files (default $VMAF_MODEL_DIR, else libvmaf's built-in models)")
	fs.IntVar(&p.CoarseSubsample, "subsample", actions.DefaultCoarseSubsample, "score every Nth frame while searching; the result is re-scored on every frame")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "maxcrf":
			o.deprecated = append(o.deprecated, "-maxcrf is now -crf-low")
		case "mincrf":
			o.deprecated = append(o.deprecated, "-mincrf is now -crf-high")
		}
	})
	p.Policy = actions.Policy(*policy)
	p.BitrateCapped = o.maxBitrateKbps > 0

	o.sourcePath = expandHome(o.sourcePath)
	o.targetPath = expandHome(o.targetPath)
	v.ModelDir = expandHome(v.ModelDir)
	v.Bin = expandHome(v.Bin)
	if strings.HasSuffix(v.Model, ".json") {
		v.Model = expandHome(v.Model)
	}
	return o, nil
}

func (o *options) validate() error {
	if !slices.Contains(validActions, o.action) {
		return fmt.Errorf("invalid action: %q, must be one of: %s", o.action, strings.Join(validActions, ", "))
	}
	// cambi is no-reference: it only reads the encode given with -o
	if o.sourcePath == "" && o.action != actionCambi {
		return errors.New("source path (-i) is required")
	}

	switch o.action {
	case actionEncode:
		if o.bitrateKbps <= 0 && o.crf <= 0 {
			return errors.New("-crf or -bitrate is required for the encode action")
		}
		if o.bitrateKbps > 0 && o.crf > 0 {
			return errors.New("-crf and -bitrate can't both be set")
		}
	default:
		if o.crf > 0 || o.bitrateKbps > 0 {
			return errors.New("-crf and -bitrate are only valid for the encode action")
		}
	}

	switch o.action {
	case actionSearch, actionInspect:
		if o.targetPath != "" {
			return fmt.Errorf("-o is not used by the %s action", o.action)
		}
	case actionOptimize, actionEncode:
		if !strings.HasSuffix(strings.ToLower(o.targetPath), ".mp4") {
			return fmt.Errorf("-o {name}.mp4 is required for the %s action", o.action)
		}
		if err := checkOutput(o.sourcePath, o.targetPath); err != nil {
			return err
		}
	case actionVmaf, actionCambi:
		if o.targetPath == "" {
			return fmt.Errorf("-o (the distorted file) is required for the %s action", o.action)
		}
	}

	if math.IsNaN(o.crf) || math.IsInf(o.crf, 0) {
		return errors.New("-crf must be a finite number")
	}
	if o.width < 0 || o.height < 0 {
		return errors.New("-width and -height can't be negative")
	}
	if o.maxBitrateKbps > 0 && o.bufferSizeKbps <= 0 {
		return errors.New("-maxbitrate needs -buffersize; encoders ignore a peak cap without a VBV buffer")
	}
	if o.search.CoarseSubsample < 1 {
		return errors.New("-subsample must be 1 or more")
	}
	if o.action == actionOptimize || o.action == actionSearch {
		return o.search.Validate()
	}
	return nil
}

// checkOutput fails fast, before a long search, on an output that can't be
// written or that is the input under another name.
func checkOutput(source, target string) error {
	// the encode writes beside a symlink's target, so check that directory
	resolved, err := commands.ResolveOutput(target)
	if err != nil {
		return err
	}
	dir := filepath.Dir(resolved)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return fmt.Errorf("output directory %s does not exist", dir)
	}
	probe, err := os.CreateTemp(dir, ".dcrf-write-check-*")
	if err != nil {
		return fmt.Errorf("output directory %s is not writable: %w", dir, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	si, serr := os.Stat(source)
	ti, terr := os.Stat(target)
	if serr == nil && terr == nil && os.SameFile(si, ti) {
		return fmt.Errorf("-o %s is the same file as -i %s", target, source)
	}
	return nil
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}
