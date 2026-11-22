//go:build unit

package inspect

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terranvigil/dynamic-crf/model"
)

func frame(pts, score float64) model.VideoFrame {
	f := model.VideoFrame{PtsTime: fmt.Sprintf("%g", pts)}
	f.Tags.SceneScore = fmt.Sprintf("%g", score)
	return f
}

func TestScenesFromFrames(t *testing.T) {
	frames := []model.VideoFrame{frame(0, 0.5), frame(4, 0.9), frame(30, 0.4)}
	scenes := scenesFromFrames(frames, 35)
	require.Len(t, scenes, 3)
	assert.InDelta(t, 4.0, scenes[0].Duration, 1e-9)
	assert.InDelta(t, MaxSceneDuration, scenes[1].Duration, 1e-9, "capped")
	assert.InDelta(t, 5.0, scenes[2].Duration, 1e-9, "last runs to the end")
	assert.InDelta(t, 0.9, scenes[1].Score, 1e-9)
	assert.Empty(t, scenesFromFrames(nil, 10))
}

// the old code sorted ascending and kept the weakest cuts
func TestSelectScenes_KeepsSharpestCutsInTimeOrder(t *testing.T) {
	var scenes []model.Scene
	for i := range 20 {
		scenes = append(scenes, model.Scene{StartPTSSec: float64(i * 10), Duration: 5, Score: float64(i%7) / 10})
	}
	picked := selectScenes(scenes, 5)
	require.Len(t, picked, 5)
	for _, s := range picked {
		assert.GreaterOrEqual(t, s.Score, 0.5, "picked a weak cut at %gs", s.StartPTSSec)
	}
	for i := 1; i < len(picked); i++ {
		assert.Less(t, picked[i-1].StartPTSSec, picked[i].StartPTSSec, "time order")
	}
	// input untouched
	assert.InDelta(t, 0.0, scenes[0].StartPTSSec, 0)
	assert.Len(t, selectScenes(scenes[:3], 5), 3)
}

func TestUniformTemporalSample(t *testing.T) {
	scenes := uniformTemporalSample(160, 15)
	require.Len(t, scenes, 15)
	assert.InDelta(t, 10.0, scenes[0].StartPTSSec, 1e-9)
	for _, s := range scenes {
		assert.InDelta(t, MinSceneDuration, s.Duration, 0)
		assert.LessOrEqual(t, s.StartPTSSec+s.Duration, 160.0)
	}
	// short sources get fewer samples, never zero
	assert.Len(t, uniformTemporalSample(9, 15), 4)
	assert.Len(t, uniformTemporalSample(1, 15), 1)
}

func TestRebaseFrames(t *testing.T) {
	frames := []model.VideoFrame{frame(3.48, 0.5), frame(10, 0.4)}
	out := rebaseFrames(frames, 1.48)
	assert.InDelta(t, 2.0, out[0].GetPtsTimeFloat64(), 1e-9)
	assert.InDelta(t, 8.52, out[1].GetPtsTimeFloat64(), 1e-9)
	assert.InDelta(t, 3.48, frames[0].GetPtsTimeFloat64(), 1e-9, "input untouched")
	assert.Equal(t, frames, rebaseFrames(frames, 0))
}
