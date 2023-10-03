//go:build integration
// +build integration

package commands

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/terranvigil/dynamic-crf/model"
	"github.com/terranvigil/dynamic-crf/testutil"
)

func TestNewFfprobeKeyframes(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "simple",
			source: "perseverance_1280.mv4",
		},
	}
	for _, tt := range tests {
		l := testutil.GetTestLogger(t)
		t.Run(tt.name, func(t *testing.T) {
			path := testutil.GetFixturePath(t, testutil.FixtureTypeMedia, tt.source)
			filepath, err := os.Open(path)
			assert.NoError(t, err)
			proc := NewFfprobeKeyframes(l, filepath)
			result, err := proc.Run(context.Background())
			assert.NoError(t, err)
			assert.NotNil(t, result)
			expected := testutil.GetFixture(t, testutil.FixtureTypeResponse, tt.source+"_ffprobe_keyframes.json")
			expectedResult := model.FfprobeFrames{}
			err = json.Unmarshal(expected, &expectedResult)
			assert.NoError(t, err)

			resultStr, err := json.MarshalIndent(result, "", "  ")
			assert.NoError(t, err)
			expectedResultStr, err := json.MarshalIndent(expectedResult, "", "  ")
			assert.NoError(t, err)
			assert.JSONEq(t, string(expectedResultStr), string(resultStr))

			assert.Equal(t, len(result.Frames), len(expectedResult.Frames))
			for i, frame := range result.Frames {
				assert.Equal(t, frame.Pts, expectedResult.Frames[i].Pts)
				assert.Equal(t, frame.Duration, expectedResult.Frames[i].Duration)
			}
		})
	}
}
