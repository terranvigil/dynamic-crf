package commands

type TranscodeConfig struct {
	VideoCodec       string
	AudioCodec       string
	AudioBitrateKbps int
	VideoBitrateKbps int
	// PLEASE NEVER USE THIS!!!
	// only for testing the legacy ABR ladder
	VideoMinBitrateKbps int
	VideoMaxBitrateKbps int
	VideoBufferSizeKbps int
	VideoCRF            int
	FPSNumerator        int
	FPSDenominator      int
	Width               int
	Height              int
	Tune                string
}
