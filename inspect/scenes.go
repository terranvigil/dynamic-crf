package inspect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sort"
	"strconv"

	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/model"
)

const (
	MinSceneDuration   = 2.0
	MaxSceneDuration   = 10.0
	MaxScenesForSample = 15
	MinScenesRequired  = 3
)

func DetectScenes(logger *slog.Logger, ctx context.Context, sourcePath string) ([]model.Scene, float64, error) {
	var scenes []model.Scene
	var fps float64
	var err error
	var sceneFrames *model.FfprobeFrames
	var metadata *model.FfprobeMetadata

	if metadata, err = commands.NewFfprobeMetadata(logger, sourcePath).Run(ctx); err != nil {
		return scenes, fps, fmt.Errorf("failed to inspect source: %s, err: %w", sourcePath, err)
	}
	if metadata.VideoStream() == nil {
		return scenes, fps, errors.New("no video stream found in source")
	}

	// the probe's rate, which is the average on variable-rate sources
	info, err := commands.VideoInfoFromMetadata(metadata, sourcePath)
	if err != nil {
		return scenes, fps, err
	}
	fps = info.FPS
	// the video's own length: audio running longer would put scenes past its end
	streamDuration := info.VideoDuration()

	if sceneFrames, err = commands.NewFfprobeScenes(logger, sourcePath, metadata.VideoStream().Index).Run(ctx); err != nil {
		return scenes, fps, fmt.Errorf("failed to probe source: %s, err: %w", sourcePath, err)
	}

	// movie= reports pts including the container's start time, but ffmpeg -ss
	// seeks relative to it (MPEG-TS often starts at 1.4s or later)
	startTime, _ := strconv.ParseFloat(metadata.Format.StartTime, 64)
	frames := rebaseFrames(sceneFrames.Frames, startTime)

	// filter out scenes that are too short
	filtered := []model.VideoFrame{}
	for i := range frames {
		if i == len(frames)-1 {
			if streamDuration-frames[i].GetPtsTimeFloat64() >= MinSceneDuration {
				filtered = append(filtered, frames[i])
			}
		} else if frames[i+1].GetPtsTimeFloat64()-frames[i].GetPtsTimeFloat64() >= MinSceneDuration {
			filtered = append(filtered, frames[i])
		}
	}
	frames = filtered

	// fallback to uniform temporal sampling if scene detection yields too few scenes
	if len(frames) < MinScenesRequired {
		logger.Info("too few scenes detected, falling back to uniform temporal sampling", "detected", len(frames))
		scenes = uniformTemporalSample(streamDuration, MaxScenesForSample)
		logger.Info("created uniform samples", "count", len(scenes))
		return scenes, fps, nil
	}

	scenes = scenesFromFrames(frames, streamDuration)
	if len(scenes) > MaxScenesForSample {
		logger.Info("reducing scenes", "found", len(scenes))
		scenes = selectScenes(scenes, MaxScenesForSample)
	}

	logger.Info("scene detection complete", "count", len(scenes))
	for i, scene := range scenes {
		logger.Debug("scene", "index", i, "start", fmt.Sprintf("%.2f", scene.StartPTSSec), "duration", fmt.Sprintf("%.2f", scene.Duration), "score", fmt.Sprintf("%.2f", scene.Score))
	}

	return scenes, fps, nil
}

// rebaseFrames shifts frame times so they're relative to the container start.
func rebaseFrames(frames []model.VideoFrame, startTime float64) []model.VideoFrame {
	if startTime == 0 {
		return frames
	}
	out := make([]model.VideoFrame, len(frames))
	for i, f := range frames {
		f.PtsTime = strconv.FormatFloat(f.GetPtsTimeFloat64()-startTime, 'f', 6, 64)
		out[i] = f
	}
	return out
}

// scenesFromFrames turns scene-change frames (in time order) into scenes that
// run to the next change, capped at MaxSceneDuration.
func scenesFromFrames(frames []model.VideoFrame, streamDuration float64) []model.Scene {
	scenes := make([]model.Scene, len(frames))
	for i := range frames {
		scenes[i] = model.Scene{
			StartPTSSec:        frames[i].GetPtsTimeFloat64(),
			Score:              frames[i].GetSceneScore(),
			StartsWithKeyframe: frames[i].KeyFrame == 1,
		}
		if i > 0 {
			scenes[i-1].Duration = math.Min(MaxSceneDuration, scenes[i].StartPTSSec-scenes[i-1].StartPTSSec)
		}
	}
	if len(scenes) > 0 {
		last := &scenes[len(scenes)-1]
		last.Duration = math.Min(MaxSceneDuration, streamDuration-last.StartPTSSec)
	}
	return scenes
}

// selectScenes keeps the n scenes with the largest scene-change scores, the
// sharpest cuts, and returns them in time order. Durations are computed before
// selection so each scene still ends at its own next cut.
func selectScenes(scenes []model.Scene, n int) []model.Scene {
	if len(scenes) <= n {
		return scenes
	}
	picked := slices.Clone(scenes)
	sort.SliceStable(picked, func(i, j int) bool {
		return picked[i].Score > picked[j].Score
	})
	picked = picked[:n]
	sort.SliceStable(picked, func(i, j int) bool {
		return picked[i].StartPTSSec < picked[j].StartPTSSec
	})
	return picked
}

// uniformTemporalSample creates evenly-spaced sample scenes across the video duration
// when scene detection doesn't yield enough results
func uniformTemporalSample(duration float64, count int) []model.Scene {
	sampleDur := MinSceneDuration
	// ensure we don't exceed total duration
	if float64(count)*sampleDur > duration {
		count = int(duration / sampleDur)
		if count < 1 {
			count = 1
		}
	}

	interval := duration / float64(count+1)
	scenes := make([]model.Scene, count)
	for i := range count {
		start := interval * float64(i+1)
		scenes[i] = model.Scene{
			StartPTSSec: start,
			Duration:    sampleDur,
		}
	}
	return scenes
}
