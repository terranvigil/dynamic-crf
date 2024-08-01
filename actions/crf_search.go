package actions

import (
	"context"
	"fmt"
	"math"
	"os"

	"github.com/rs/zerolog"
	"github.com/terranvigil/dynamic-crf/commands"
	"github.com/terranvigil/dynamic-crf/inspect"
	"github.com/terranvigil/dynamic-crf/model"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// CrfSearch will perform an interpolation search over CRF values to find the
// closest match to the target VMAF
type CrfSearch struct {
	logger          zerolog.Logger
	sourcePath      string
	targetVMAF      float64
	crfInitial      int
	crfMin          int
	crfMax          int
	tolerance       float64
	transcodeConfig commands.TranscodeConfig
}

func NewCrfSearch(logger zerolog.Logger, source string, targetVMAF float64, crfInitial int, crfMin int, crfMax int, tolerance float64, transcodeConfig commands.TranscodeConfig) *CrfSearch {
	return &CrfSearch{
		logger:          logger,
		sourcePath:      source,
		targetVMAF:      targetVMAF,
		crfInitial:      crfInitial,
		crfMin:          crfMin,
		crfMax:          crfMax,
		tolerance:       tolerance,
		transcodeConfig: transcodeConfig,
	}
}

func (c *CrfSearch) Run(ctx context.Context) (selected int, vmaf float64, err error) {
	// 1. get list of scenes
	// 2. create scaled sample of source
	// 3. perform interpolated search, looking for closest match to target vmaf
	//    a. starting with min CRF value, encode sample
	//    b. calc vmaf of "distorted" output
	//    c. unless within tolerance, increase or decrease CRF and repeat

	c.logger.Info().Msg("running dynamic-crf search")

	var source, sampleEncode *os.File
	if source, err = os.Open(c.sourcePath); err != nil {
		c.logger.Fatal().Err(err).Msgf("failed to open source: %s", c.sourcePath)
	}
	defer source.Close()

	var metadata *model.MediaInfo
	if metadata, err = commands.NewMediaInfo(c.logger, c.sourcePath).Run(ctx); err != nil {
		return 0, 0.0, fmt.Errorf("failed to get mediainfo of test output, err: %w", err)
	}
	if metadata.GetContainer().Duration < 60*1000 { //nolint:mnd
		c.logger.Info().Msg("source is less than 60 seconds, skipping creation of shots sample")
		sampleEncode = source
	} else {
		if sampleEncode, err = c.createSceneSample(ctx); err != nil {
			return 0, 0.0, fmt.Errorf("failed to create scene sample, err: %w", err)
		}
		defer os.Remove(sampleEncode.Name())
	}

	// TODO this needs to be adjusted per codec

	// get vmaf of initial CRF, where we expect a high score
	// if initial CRF is lower than target, use for min CRF
	// else use for max CRF
	// get vmaf of lowest CRF
	// get vmaf of highest CRF
	// calc interpolated next CRF
	// if within threshold, return CRF
	// if not, repeat using interpolated CRF as new min or max

	var initialScore float64
	if initialScore, err = c.runScore(ctx, sampleEncode.Name(), c.crfInitial); err != nil {
		return selected, vmaf, err
	} else if checkScore(initialScore, c.targetVMAF, c.tolerance) {
		vmaf = initialScore
		selected = c.crfInitial
		c.logger.Info().Msgf("found vmaf: %.2f for crf: %d", vmaf, selected)
		return selected, vmaf, nil
	} else if initialScore > c.targetVMAF {
		c.crfMax = c.crfInitial
	} else {
		c.crfMin = c.crfInitial
	}

	low := 0
	high := c.crfMin - c.crfMax
	scores := make([]float64, high+1)
	if c.crfMax == c.crfInitial {
		scores[high] = initialScore
	} else if c.crfMin == c.crfInitial {
		scores[low] = initialScore
	}

	if scores[low] == 0 {
		if scores[low], err = c.runScore(ctx, sampleEncode.Name(), c.crfMin); err != nil {
			return selected, vmaf, err
		}
		if checkScore(scores[low], c.targetVMAF, c.tolerance) {
			vmaf = scores[low]
			selected = c.crfMin
			c.logger.Info().Msgf("found vmaf: %.2f for crf: %d", vmaf, selected)
			return selected, vmaf, err
		}
	}

	if scores[high] == 0 {
		if scores[high], err = c.runScore(ctx, sampleEncode.Name(), c.crfMax); err != nil {
			return selected, vmaf, nil
		}
		if checkScore(scores[high], c.targetVMAF, c.tolerance) {
			vmaf = scores[high]
			selected = c.crfMax
			c.logger.Info().Msgf("found vmaf: %.2f for crf: %d", vmaf, selected)
			return selected, vmaf, err
		}
	}

	c.logger.Info().Msgf("Initial VMAF range: low: %.2f, high: %.2f", scores[low], scores[high])

	if scores[high] < c.targetVMAF {
		selected = c.crfMax
		vmaf = scores[high]
		c.logger.Info().Msgf("target vmaf: %.2f is higher than vmaf of %f for max crf: %d, selecting max crf", c.targetVMAF, vmaf, c.crfMax)
		return selected, vmaf, err
	}

	// interpolated search
	// from wiki: `int pos = low + (((target - arr[low]) * (high - low)) / (arr[high] - arr[low]));`
	// TODO: crf -> vmaf is logarirthmic, adjust search
	var lastPos, curPos, curCRF int
	for low <= high && c.targetVMAF >= scores[low] && c.targetVMAF <= scores[high] {
		curPos = low + int(math.Round((((c.targetVMAF - scores[low]) * float64(high-low)) / (scores[high] - scores[low]))))
		if curPos == lastPos {
			break
		}
		curCRF = c.crfMin - curPos

		c.logger.Info().Msgf("searching position: %d, crf: %d", curPos, curCRF)

		if scores[curPos], err = c.runScore(ctx, sampleEncode.Name(), curCRF); err != nil {
			return selected, vmaf, err
		}

		if checkScore(scores[curPos], c.targetVMAF, c.tolerance) {
			break
		}

		if scores[curPos] > c.targetVMAF {
			// overshot, search left
			high = curPos
		} else {
			// undershot, search right
			low = curPos
		}
		lastPos = curPos
	}
	selected = curCRF
	vmaf = scores[curPos]
	c.logger.Info().Msgf("found vmaf: %.2f for crf: %d", vmaf, selected)

	return selected, vmaf, err
}

func (c *CrfSearch) runScore(ctx context.Context, samplePath string, crf int) (float64, error) {
	currentConfig := c.transcodeConfig
	currentConfig.VideoCRF = crf
	score, averageBitrateKBPS, maxBitrateKBPS, streamSize, err := NewVMAFEncodeScore(c.logger, currentConfig, samplePath).Run(ctx)
	if err != nil {
		return 0, err
	}

	streamSizeKB := message.NewPrinter(language.English).Sprintf("%d", streamSize)
	c.logger.Info().Msgf("score: %.2f, avgBitrate: %dKbps, maxBitrate: %dKbps, buffer-size: %dKbs, file-size: %sKB", score, averageBitrateKBPS, maxBitrateKBPS, currentConfig.VideoBufferSizeKbps, streamSizeKB)

	return score, nil
}

func checkScore(vmaf float64, targetVmaf float64, tolerance float64) bool {
	return math.Abs(vmaf-targetVmaf) < tolerance
}

func (c *CrfSearch) createSceneSample(ctx context.Context) (*os.File, error) {
	var err error
	var scenes []model.Scene
	var fps float64
	var sampleEncode *os.File

	if sampleEncode, err = os.CreateTemp("", "sample_*.mp4"); err != nil {
		c.logger.Fatal().Err(err).Msgf("failed to create temp file for encoding sample")
	}

	if scenes, fps, err = inspect.DetectScenes(c.logger, ctx, c.sourcePath); err != nil {
		err = fmt.Errorf("failed to detect scenes in source, err: %w", err)
		return nil, err
	}
	if err = commands.NewFfmpegSampler(c.logger, c.sourcePath, sampleEncode.Name(), fps, scenes).Run(ctx); err != nil {
		err = fmt.Errorf("failed to sample source, err: %w", err)
		return nil, err
	}

	return sampleEncode, nil
}
