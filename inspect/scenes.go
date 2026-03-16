package inspect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
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

	var source *os.File
	if source, err = os.Open(sourcePath); err != nil {
		return scenes, fps, fmt.Errorf("failed to open source: %s, err: %w", sourcePath, err)
	}
	defer source.Close() //nolint:errcheck

	if metadata, err = commands.NewFfprobeMetadata(logger, source).Run(ctx); err != nil {
		return scenes, fps, fmt.Errorf("failed to inspect source: %s, err: %w", sourcePath, err)
	}
	if metadata.VideoStream() == nil {
		return scenes, fps, errors.New("no video stream found in source")
	}

	if fps, err = model.FractionToFloat(metadata.VideoStream().RFrameRate); err != nil {
		return scenes, fps, fmt.Errorf("failed to parse fps from source, found: %s, err: %w", metadata.VideoStream().RFrameRate, err)
	}
	var streamDuration float64
	if streamDuration, err = strconv.ParseFloat(metadata.Format.Duration, 64); err != nil {
		return scenes, fps, fmt.Errorf("failed to parse video duration from source, found: %s, err: %w", metadata.Format.Duration, err)
	}

	if sceneFrames, err = commands.NewFfprobeScenes(logger, source).Run(ctx); err != nil {
		return scenes, fps, fmt.Errorf("failed to probe source: %s, err: %w", sourcePath, err)
	}

	frames := sceneFrames.Frames

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

	if len(frames) > MaxScenesForSample {
		logger.Info("reducing scenes", "found", len(frames))

		sort.SliceStable(frames, func(i, j int) bool {
			return frames[i].Pts < frames[j].Pts
		})

		// select the most significant scenes
		sort.SliceStable(frames, func(i, j int) bool {
			return frames[i].GetSceneScore() < frames[j].GetSceneScore()
		})
		frames = frames[:MaxScenesForSample]

		// put them back in time order
		sort.SliceStable(frames, func(i, j int) bool {
			return frames[i].Pts < frames[j].Pts
		})
	}

	scenes = make([]model.Scene, len(frames))
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
	// last scene
	scenes[len(scenes)-1].Duration = math.Min(MaxSceneDuration, streamDuration-scenes[len(scenes)-1].StartPTSSec)

	logger.Info("scene detection complete", "count", len(scenes))
	for i, scene := range scenes {
		logger.Debug("scene", "index", i, "start", fmt.Sprintf("%.2f", scene.StartPTSSec), "duration", fmt.Sprintf("%.2f", scene.Duration), "score", fmt.Sprintf("%.2f", scene.Score))
	}

	return scenes, fps, nil
}

// uniformTemporalSample creates evenly-spaced sample scenes across the video duration
// when scene detection doesn't yield enough results
func uniformTemporalSample(duration float64, count int) []model.Scene {
	sampleDur := math.Min(MaxSceneDuration, MinSceneDuration)
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
