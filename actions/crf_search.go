package actions

import (
	"context"
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

	if sampleEncode, err = os.CreateTemp("", "sample_*.mp4"); err != nil {
		c.logger.Fatal().Err(err).Msgf("failed to create temp file for encoding sample")
	}
	defer os.Remove(sampleEncode.Name())

	var scenes []model.Scene
	var fps float64
	if scenes, fps, err = inspect.DetectScenes(c.logger, ctx, c.sourcePath); err != nil {
		c.logger.Fatal().Err(err).Msgf("failed to detect scenes in source: %s", c.sourcePath)
	}

	if err = commands.NewFfmpegSampler(c.logger, c.sourcePath, sampleEncode.Name(), fps, scenes).Run(ctx); err != nil {
		c.logger.Fatal().Err(err).Msgf("failed to sample source: %s", c.sourcePath)
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

	initialScore := 0.0
	if initialScore, err = c.runScore(ctx, sampleEncode.Name(), c.crfInitial); err != nil {
		return
	} else if checkScore(c.logger, initialScore, c.targetVMAF, c.tolerance, c.crfInitial) {
		vmaf = initialScore
		selected = c.crfInitial
		c.logger.Info().Msgf("found vmaf: %.2f for crf: %d", vmaf, selected)
		return
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
			return
		}
		if checkScore(c.logger, scores[low], c.targetVMAF, c.tolerance, c.crfMin) {
			vmaf = scores[low]
			selected = c.crfMin
			c.logger.Info().Msgf("found vmaf: %.2f for crf: %d", vmaf, selected)
			return
		}
	}

	if scores[high] == 0 {
		if scores[high], err = c.runScore(ctx, sampleEncode.Name(), c.crfMax); err != nil {
			return
		}
		if checkScore(c.logger, scores[high], c.targetVMAF, c.tolerance, c.crfMax) {
			vmaf = scores[high]
			selected = c.crfMax
			c.logger.Info().Msgf("found vmaf: %.2f for crf: %d", vmaf, selected)
			return
		}
	}

	c.logger.Info().Msgf("Initial VMAF range: low: %.2f, high: %.2f", scores[low], scores[high])

	if scores[high] < c.targetVMAF {
		selected = c.crfMax
		vmaf = scores[high]
		c.logger.Info().Msgf("target vmaf: %.2f is higher than vmaf of %f for max crf: %d, selecting max crf", c.targetVMAF, vmaf, c.crfMax)
		return
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
			return
		}

		if checkScore(c.logger, scores[curPos], c.targetVMAF, c.tolerance, curCRF) {
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

	return
}

func (c *CrfSearch) runScore(ctx context.Context, samplePath string, crf int) (float64, error) {
	currentConfig := c.transcodeConfig
	currentConfig.VideoCRF = crf
	score, averageBitrateKBPS, streamSize, err := NewVMAFScore(c.logger, currentConfig, samplePath).Run(ctx)
	if err != nil {
		return 0, err
	}

	streamSizeKB := message.NewPrinter(language.English).Sprintf("%d", streamSize)
	c.logger.Info().Msgf("score: %.2f, bitrate: %dKbps, size: %sKB", score, averageBitrateKBPS, streamSizeKB)

	return score, nil
}

func checkScore(logger zerolog.Logger, vmaf float64, targetVmaf float64, tolerance float64, crf int) bool {
	return math.Abs(vmaf-targetVmaf) < tolerance
}
