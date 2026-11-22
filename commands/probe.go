package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"regexp"
	"strconv"

	"github.com/terranvigil/dynamic-crf/model"
)

// relative gap between r_frame_rate and avg_frame_rate past which a source is
// treated as variable rate
const vfrTolerance = 0.05

// VideoInfo is the subset of stream metadata that scoring and sampling need.
type VideoInfo struct {
	Width    int
	Height   int
	FPS      float64
	FPSRatio string // r_frame_rate as ffprobe reports it, e.g. "24000/1001"
	PixFmt   string
	BitDepth int
	// container duration in seconds; the longest stream, audio included
	Duration  float64
	SizeBytes int64
	// VFR is set when r_frame_rate and avg_frame_rate disagree; FPS is then the average
	VFR bool
	// VideoFrames is the video stream's frame count when the container
	// records it, else an estimate from the video stream's own duration
	VideoFrames int
}

// VideoDuration is the video stream's own length in seconds, falling back
// to the container's when the stream doesn't say.
func (v *VideoInfo) VideoDuration() float64 {
	if v.VideoFrames > 0 && v.FPS > 0 {
		return float64(v.VideoFrames) / v.FPS
	}
	return v.Duration
}

// BitrateKbps is the average total bitrate from file size and duration.
func (v *VideoInfo) BitrateKbps() int {
	if v.Duration <= 0 {
		return 0
	}
	return int(float64(v.SizeBytes) * 8 / v.Duration / 1000) //nolint:mnd
}

// ProbeVideo reads the first video stream's geometry, rate and depth with ffprobe.
func ProbeVideo(ctx context.Context, logger *slog.Logger, path string) (*VideoInfo, error) {
	meta, err := NewFfprobeMetadata(logger, path).Run(ctx)
	if err != nil {
		return nil, err
	}
	return VideoInfoFromMetadata(meta, path)
}

// VideoInfoFromMetadata extracts VideoInfo from ffprobe output.
func VideoInfoFromMetadata(meta *model.FfprobeMetadata, path string) (*VideoInfo, error) {
	vs := meta.VideoStream()
	if vs == nil {
		return nil, fmt.Errorf("no video stream in %s", path)
	}
	if vs.Width <= 0 || vs.Height <= 0 {
		return nil, fmt.Errorf("invalid video dimensions %dx%d in %s", vs.Width, vs.Height, path)
	}
	// ffmpeg auto-rotates on decode, so what the encoder and vmaf see is
	// the displayed size, not the coded one
	w, h := vs.Width, vs.Height
	if r := math.Mod(math.Abs(vs.Rotation()), 180); r > 45 && r < 135 {
		w, h = h, w
	}
	info := &VideoInfo{
		Width:    w,
		Height:   h,
		FPSRatio: vs.RFrameRate,
		PixFmt:   vs.PixFmt,
		BitDepth: bitDepth(vs.PixFmt, vs.BitsPerRawSample),
	}
	if vs.RFrameRate != "" {
		fps, err := model.FractionToFloat(vs.RFrameRate)
		if err != nil {
			return nil, fmt.Errorf("parse frame rate of %s: %w", path, err)
		}
		info.FPS = fps
	}
	// r_frame_rate is the lowest rate that represents every timestamp, which on
	// variable-rate sources can be far above the real rate (and would pick the
	// _hfr model). trust the average when they disagree by more than a little.
	if avg, err := model.FractionToFloat(vs.AvgFrameRate); err == nil && avg > 0 &&
		(info.FPS <= 0 || math.Abs(info.FPS-avg)/avg > vfrTolerance) {
		info.VFR = info.FPS > 0
		info.FPS, info.FPSRatio = avg, vs.AvgFrameRate
	}
	info.VideoFrames = videoFrames(vs, info.FPS)
	if meta.Format.Duration != "" {
		d, err := strconv.ParseFloat(meta.Format.Duration, 64)
		if err != nil {
			return nil, fmt.Errorf("parse duration of %s: %w", path, err)
		}
		info.Duration = d
	}
	if meta.Format.Size != "" {
		if sz, err := strconv.ParseInt(meta.Format.Size, 10, 64); err == nil {
			info.SizeBytes = sz
		}
	}
	if info.SizeBytes == 0 {
		if st, err := os.Stat(path); err == nil {
			info.SizeBytes = st.Size()
		}
	}
	if info.FPS <= 0 {
		return nil, errors.New("could not determine frame rate of " + path)
	}
	return info, nil
}

// videoFrames reads the video stream's own length, which the container
// duration overstates whenever audio runs longer than video.
func videoFrames(vs *model.FfprobeStream, fps float64) int {
	byDuration := 0
	if d, err := strconv.ParseFloat(vs.Duration, 64); err == nil && d > 0 {
		byDuration = int(math.Round(d * fps))
	} else if d, ok := parseClock(vs.Tags.Duration); ok {
		byDuration = int(math.Round(d * fps))
	}
	// nb_frames counts every stored frame, including ones an mp4 edit list
	// (a stream-copy trim) hides, while the stream duration honors the edit
	n, err := strconv.Atoi(vs.NbFrames)
	switch {
	case err == nil && n > 0 && (byDuration == 0 || n <= byDuration):
		return n
	default:
		return byDuration
	}
}

// parseClock reads HH:MM:SS.fraction.
func parseClock(s string) (float64, bool) {
	var h, m int
	var sec float64
	if _, err := fmt.Sscanf(s, "%d:%d:%f", &h, &m, &sec); err != nil {
		return 0, false
	}
	return float64(h*3600+m*60) + sec, true //nolint:mnd
}

// pixel formats above 8 bits carry the depth as a suffix, e.g. yuv420p10le, p010le
var pixFmtDepthRE = regexp.MustCompile(`(\d+)(le|be)$`)

// bitDepth prefers bits_per_raw_sample and falls back to the pixel format name,
// since ffprobe leaves the former empty for many decoders.
func bitDepth(pixFmt, bitsPerRawSample string) int {
	if n, err := strconv.Atoi(bitsPerRawSample); err == nil && n > 0 {
		return n
	}
	if m := pixFmtDepthRE.FindStringSubmatch(pixFmt); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 8 {
			return n
		}
	}
	return 8 //nolint:mnd
}
