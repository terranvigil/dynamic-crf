package inspect

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/model"
)

const (
	MinSceneDuration   = 2.0
	MaxScenesForSample = 15
)

func DetectScenes(logger zerolog.Logger, ctx context.Context, sourcePath string) (scenes []model.Scene, fps float64, err error) {
	var sceneFrames *model.FfprobeFrames
	var metadata *model.FfprobeMetadata

	var source *os.File
	if source, err = os.Open(sourcePath); err != nil {
		err = fmt.Errorf("failed to open source: %s, err: %w", sourcePath, err)
		return
	}

	if metadata, err = commands.NewFfprobeMetadata(logger, source).Run(ctx); err != nil {
		err = fmt.Errorf("failed to inspect source: %s, err: %w", sourcePath, err)
		return
	}
	if metadata.VideoStream() == nil {
		err = fmt.Errorf("no video stream found in source")
		return
	}

	if fps, err = model.FractionToFloat(metadata.VideoStream().RFrameRate); err != nil {
		err = fmt.Errorf("failed to parse fps from source, found: %s, err: %w", metadata.VideoStream().RFrameRate, err)
		return
	}
	var streamDuration float64
	if streamDuration, err = strconv.ParseFloat(metadata.Format.Duration, 64); err != nil {
		err = fmt.Errorf("failed to parse video duration from source, found: %s, err: %w", metadata.Format.Duration, err)
		return
	}

	if sceneFrames, err = commands.NewFfprobeScenes(logger, source).Run(ctx); err != nil {
		err = fmt.Errorf("failed to probe source: %s, err: %w", sourcePath, err)
		return
	}

	frames := sceneFrames.Frames

	// filter out scenes that are too short
	filtered := []model.VideoFrame{}
	for i := 0; i < len(frames); i++ {
		if i == len(frames)-1 {
			if streamDuration-frames[i].GetPtsTimeFloat64() >= MinSceneDuration {
				filtered = append(filtered, frames[i])
			}
		} else if frames[i+1].GetPtsTimeFloat64()-frames[i].GetPtsTimeFloat64() >= MinSceneDuration {
			filtered = append(filtered, frames[i])
		}
	}
	frames = filtered

	if len(frames) > MaxScenesForSample {
		logger.Info().Msgf("found %d scenes, reducing", len(frames))

		// should already be in time order, but just in case
		sort.SliceStable(frames, func(i, j int) bool {
			return frames[i].Pts < frames[j].Pts
		})

		// we want the most significant scenes
		sort.SliceStable(frames, func(i, j int) bool {
			logger.Debug().Msgf("score: %.2f", frames[i].GetSceneScore())
			return frames[i].GetSceneScore() < frames[j].GetSceneScore()
		})
		frames = frames[:MaxScenesForSample]

		// put them back in time order
		sort.SliceStable(frames, func(i, j int) bool {
			return frames[i].Pts < frames[j].Pts
		})
	}

	scenes = make([]model.Scene, len(frames))
	for i, frame := range frames {
		scenes[i] = model.Scene{
			StartPTSSec:        frame.GetPtsTimeFloat64(),
			Score:              frame.GetSceneScore(),
			StartsWithKeyframe: frame.KeyFrame == 1,
		}
		if i > 0 {
			scenes[i-1].Duration = math.Min(10.0, scenes[i].StartPTSSec-scenes[i-1].StartPTSSec)
		}
	}
	// last scene
	scenes[len(scenes)-1].Duration = math.Min(10.0, streamDuration-scenes[len(scenes)-1].StartPTSSec)

	logger.Info().Msgf("found %d scenes", len(scenes))
	for i, scene := range scenes {
		logger.Info().Msgf("scene %d, start: %.2f, dur: %.2f, score: %.2f", i, scene.StartPTSSec, scene.Duration, scene.Score)
	}

	return scenes, fps, nil
}
