package commands

import "strconv"

// AudioNone drops audio from the output.
const AudioNone = "none"

type TranscodeConfig struct {
	VideoCodec string
	// AudioCodec is an ffmpeg audio encoder, "copy", or empty/AudioNone to drop audio
	AudioCodec       string
	AudioBitrateKbps int
	VideoBitrateKbps int
	// PLEASE NEVER USE THIS!!!
	// only for testing the legacy ABR ladder
	VideoMinBitrateKbps int
	VideoMaxBitrateKbps int
	VideoBufferSizeKbps int
	// VideoCRF is the constant rate factor. Zero means unset. x264 and x265
	// accept fractional values.
	VideoCRF       float64
	FPSNumerator   int
	FPSDenominator int
	Width          int
	Height         int
	Tune           string
	Preset         string
	// PixFmt is the output pixel format; empty leaves it to the encoder
	PixFmt string
}

// FormatCRF renders a CRF without trailing zeros: 23, 23.5, 23.25.
func FormatCRF(crf float64) string {
	return strconv.FormatFloat(crf, 'f', -1, 64)
}
