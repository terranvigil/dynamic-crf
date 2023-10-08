package model

import (
	"regexp"
	"strconv"
	"strings"
)

type TrackType string

const (
	TrackTypeVideo    TrackType = "video"
	TrackTypeAudio    TrackType = "audio"
	TrackTypeSubtitle TrackType = "subtitle"
	TrackTypeCaption  TrackType = "caption"
	TrackTypeImage    TrackType = "image"
	TrackTypeMenu     TrackType = "menu"
	TrackTypeGeneral  TrackType = "general"

	MediaInfoTrackTypeGeneral = "General"
	MediaInfoTrackTypeVideo   = "Video"
	MediaInfoTrackTypeAudio   = "Audio"
	MediaInfoTrackTypeText    = "Text"
	MediaInfoTrackTypeImage   = "Image"
	MediaInfoTrackTypeMenu    = "Menu"
)

type MediaInfo struct {
	CreatingLibrary struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		URL     string `json:"url"`
	} `json:"creatingLibrary"`
	Media struct {
		Ref    string            `json:"@ref"`
		Tracks []*MediaInfoTrack `json:"track"`
	} `json:"media"`
}

type MediaInfoTrack struct {
	TrackTypeMixedCase        string  `json:"@type"`
	Type                      string  `json:"Type,omitempty"`
	Count                     string  `json:"Count"`
	ID                        string  `json:"ID,omitempty"`
	IDString                  string  `json:"ID_String,omitempty"`
	StreamCount               string  `json:"StreamCount"`
	StreamKind                string  `json:"StreamKind"`
	StreamKindString          string  `json:"StreamKind_String"`
	StreamKindID              string  `json:"StreamKindID"`
	VideoCount                string  `json:"VideoCount,omitempty"`
	AudioCount                string  `json:"AudioCount,omitempty"`
	OtherCount                string  `json:"OtherCount,omitempty"`
	VideoFormatList           string  `json:"Video_Format_List,omitempty"`
	VideoFormatWithHintList   string  `json:"Video_Format_WithHint_List,omitempty"`
	VideoCodecList            string  `json:"Video_Codec_List,omitempty"`
	AudioFormatList           string  `json:"Audio_Format_List,omitempty"`
	AudioFormatWithHintList   string  `json:"Audio_Format_WithHint_List,omitempty"`
	AudioCodecList            string  `json:"Audio_Codec_List,omitempty"`
	AudioLanguageList         string  `json:"Audio_Language_List,omitempty"`
	OtherFormatList           string  `json:"Other_Format_List,omitempty"`
	OtherFormatWithHintList   string  `json:"Other_Format_WithHint_List,omitempty"`
	OtherCodecList            string  `json:"Other_Codec_List,omitempty"`
	CompleteName              string  `json:"CompleteName,omitempty"`
	FileNameExtension         string  `json:"FileNameExtension,omitempty"`
	FileName                  string  `json:"FileName,omitempty"`
	FileExtension             string  `json:"FileExtension,omitempty"`
	Format                    string  `json:"Format"`
	FormatString              string  `json:"Format_String"`
	FormatExtensions          string  `json:"Format_Extensions,omitempty"`
	FormatCommercial          string  `json:"Format_Commercial"`
	FormatProfile             string  `json:"Format_Profile,omitempty"`
	InternetMediaType         string  `json:"InternetMediaType,omitempty"`
	CodecID                   string  `json:"CodecID,omitempty"`
	CodecIDString             string  `json:"CodecID_String,omitempty"`
	CodecIDURL                string  `json:"CodecID_Url,omitempty"`
	CodecIDVersion            string  `json:"CodecID_Version,omitempty"`
	CodecIDCompatible         string  `json:"CodecID_Compatible,omitempty"`
	FileSize                  string  `json:"FileSize,omitempty"`
	FileSizeString            string  `json:"FileSize_String,omitempty"`
	FileSizeString1           string  `json:"FileSize_String1,omitempty"`
	FileSizeString2           string  `json:"FileSize_String2,omitempty"`
	FileSizeString3           string  `json:"FileSize_String3,omitempty"`
	FileSizeString4           string  `json:"FileSize_String4,omitempty"`
	Duration                  float64 `json:"Duration,string"`
	DurationString            string  `json:"Duration_String"`
	DurationString1           string  `json:"Duration_String1"`
	DurationString2           string  `json:"Duration_String2"`
	DurationString3           string  `json:"Duration_String3"`
	DurationString4           string  `json:"Duration_String4,omitempty"`
	DurationString5           string  `json:"Duration_String5"`
	OverallBitRate            string  `json:"OverallBitRate,omitempty"`
	OverallBitRateString      string  `json:"OverallBitRate_String,omitempty"`
	FrameRate                 float32 `json:"FrameRate,string"`
	FrameRateString           string  `json:"FrameRate_String"`
	FrameRateMode             string  `json:"FrameRate_Mode,omitempty"`
	FrameRateModeString       string  `json:"FrameRate_Mode_String,omitempty"`
	FrameRateNum              int     `json:"FrameRate_Num,string,omitempty"`
	FrameRateDen              int     `json:"FrameRate_Den,string,omitempty"`
	GOPOpenClosed             string  `json:"Gop_OpenClosed,omitempty"`
	FrameCount                string  `json:"FrameCount,omitempty"`
	ScanTypeStoreMethod       string  `json:"ScanType_StoreMethod,omitempty"`
	ScanTypeStoreMethodString string  `json:"ScanType_StoreMethod_String,omitempty"`
	ScanOrder                 string  `json:"ScanOrder,omitempty"`
	ScanOrderString           string  `json:"ScanOrder_String,omitempty"`
	StreamSize                int     `json:"StreamSize,string,omitempty"`
	StreamSizeString          string  `json:"StreamSize_String,omitempty"`
	StreamSizeString1         string  `json:"StreamSize_String1,omitempty"`
	StreamSizeString2         string  `json:"StreamSize_String2,omitempty"`
	StreamSizeString3         string  `json:"StreamSize_String3,omitempty"`
	StreamSizeString4         string  `json:"StreamSize_String4,omitempty"`
	StreamSizeString5         string  `json:"StreamSize_String5,omitempty"`
	StreamSizeProportion      string  `json:"StreamSize_Proportion,omitempty"`
	HeaderSize                string  `json:"HeaderSize,omitempty"`
	DataSize                  string  `json:"DataSize,omitempty"`
	FooterSize                string  `json:"FooterSize,omitempty"`
	IsStreamable              string  `json:"IsStreamable,omitempty"`
	EncodedDate               string  `json:"Encoded_Date,omitempty"`
	TaggedDate                string  `json:"Tagged_Date,omitempty"`
	EncodedApplication        string  `json:"Encoded_Application,omitempty"`
	EncodedApplicationString  string  `json:"Encoded_Application_String,omitempty"`
	EncodedLibrary            string  `json:"Encoded_Library,omitempty"`
	EncodedLibraryString      string  `json:"Encoded_Library_String,omitempty"`
	EncodedLibraryName        string  `json:"Encoded_Library_Name,omitempty"`
	Extra                     struct {
		ComAppleQuicktimeSoftware string `json:"com_apple_quicktime_software"`
		CodecConfigurationBox     string `json:"CodecConfigurationBox"`
		SourceDelay               string `json:"Source_Delay"`
		SourceDelaySource         string `json:"Source_Delay_Source"`
		EncodedDate               string `json:"Encoded_Date"`
		TaggedDate                string `json:"Tagged_Date"`
	} `json:"extra,omitempty"`
	StreamOrder                    string  `json:"StreamOrder,omitempty"`
	FormatInfo                     string  `json:"Format_Info,omitempty"`
	FormatURL                      string  `json:"Format_Url,omitempty"`
	FormatLevel                    string  `json:"Format_Level,omitempty"`
	FormatSettings                 string  `json:"Format_Settings,omitempty"`
	FormatSettingsCABAC            string  `json:"Format_Settings_CABAC,omitempty"`
	FormatSettingsCABACString      string  `json:"Format_Settings_CABAC_String,omitempty"`
	FormatSettingsRefFrames        string  `json:"Format_Settings_RefFrames,omitempty"`
	FormatSettingsRefFramesString  string  `json:"Format_Settings_RefFrames_String,omitempty"`
	CodecIDInfo                    string  `json:"CodecID_Info,omitempty"`
	BitRate                        int     `json:"BitRate,string,omitempty"`
	BitRateString                  string  `json:"BitRate_String,omitempty"`
	BitRateMaximum                 int     `json:"BitRate_Maximum,string,omitempty"`
	BitRateMaximumString           string  `json:"BitRate_Maximum_String,omitempty"`
	Width                          int     `json:"Width,string,omitempty"`
	WidthString                    string  `json:"Width_String,omitempty"`
	Height                         int     `json:"Height,string,omitempty"`
	HeightString                   string  `json:"Height_String,omitempty"`
	SampledWidth                   int     `json:"Sampled_Width,string,omitempty"`
	SampledHeight                  int     `json:"Sampled_Height,string,omitempty"`
	PixelAspectRatio               float64 `json:"PixelAspectRatio,string,omitempty"`
	DisplayAspectRatio             string  `json:"DisplayAspectRatio,omitempty"`
	DisplayAspectRatioString       string  `json:"DisplayAspectRatio_String,omitempty"`
	Rotation                       string  `json:"Rotation,omitempty"`
	ColorSpace                     string  `json:"ColorSpace,omitempty"`
	ChromaSubsampling              string  `json:"ChromaSubsampling,omitempty"`
	ChromaSubsamplingString        string  `json:"ChromaSubsampling_String,omitempty"`
	BitDepth                       int     `json:"BitDepth,string,omitempty"`
	BitDepthString                 string  `json:"BitDepth_String,omitempty"`
	ScanType                       string  `json:"ScanType,omitempty"`
	ScanTypeString                 string  `json:"ScanType_String,omitempty"`
	BitsPixelFrame                 string  `json:"BitsPixel_Frame,omitempty"`
	Delay                          string  `json:"Delay,omitempty"`
	DelayString                    string  `json:"Delay_String,omitempty"`
	DelayString1                   string  `json:"Delay_String1,omitempty"`
	DelayString2                   string  `json:"Delay_String2,omitempty"`
	DelayString3                   string  `json:"Delay_String3,omitempty"`
	DelaySettings                  string  `json:"Delay_Settings,omitempty"`
	DelayDropFrame                 string  `json:"Delay_DropFrame,omitempty"`
	DelaySource                    string  `json:"Delay_Source,omitempty"`
	DelaySourceString              string  `json:"Delay_Source_String,omitempty"`
	Title                          string  `json:"Title,omitempty"`
	ColourDescriptionPresent       string  `json:"colour_description_present,omitempty"`
	ColourDescriptionPresentSource string  `json:"colour_description_present_Source,omitempty"`
	ColourRange                    string  `json:"colour_range,omitempty"`
	ColourRangeSource              string  `json:"colour_range_Source,omitempty"`
	ColourPrimaries                string  `json:"colour_primaries,omitempty"`
	ColourPrimariesSource          string  `json:"colour_primaries_Source,omitempty"`
	TransferCharacteristics        string  `json:"transfer_characteristics,omitempty"`
	TransferCharacteristicsSource  string  `json:"transfer_characteristics_Source,omitempty"`
	MatrixCoefficients             string  `json:"matrix_coefficients,omitempty"`
	MatrixCoefficientsSource       string  `json:"matrix_coefficients_Source,omitempty"`
	FormatAdditionalFeatures       string  `json:"Format_AdditionalFeatures,omitempty"`
	SourceDuration                 float64 `json:"Source_Duration,string,omitempty"`
	SourceDurationString           string  `json:"Source_Duration_String,omitempty"`
	SourceDurationString1          string  `json:"Source_Duration_String1,omitempty"`
	SourceDurationString2          string  `json:"Source_Duration_String2,omitempty"`
	SourceDurationString3          string  `json:"Source_Duration_String3,omitempty"`
	BitRateMode                    string  `json:"BitRate_Mode,omitempty"`
	BitRateModeString              string  `json:"BitRate_Mode_String,omitempty"`
	Channels                       int     `json:"Channels,string,omitempty"`
	ChannelsString                 string  `json:"Channels_String,omitempty"`
	ChannelPositions               string  `json:"ChannelPositions,omitempty"`
	ChannelPositionsString2        string  `json:"ChannelPositions_String2,omitempty"`
	ChannelLayout                  string  `json:"ChannelLayout,omitempty"`
	SamplesPerFrame                string  `json:"SamplesPerFrame,omitempty"`
	SamplingRate                   int     `json:"SamplingRate,string,omitempty"`
	SamplingRateString             string  `json:"SamplingRate_String,omitempty"`
	SamplingCount                  string  `json:"SamplingCount,omitempty"`
	SourceFrameCount               string  `json:"Source_FrameCount,omitempty"`
	CompressionMode                string  `json:"Compression_Mode,omitempty"`
	CompressionModeString          string  `json:"Compression_Mode_String,omitempty"`
	VideoDelay                     string  `json:"Video_Delay,omitempty"`
	VideoDelayString3              string  `json:"Video_Delay_String3,omitempty"`
	SourceStreamSize               string  `json:"Source_StreamSize,omitempty"`
	SourceStreamSizeString         string  `json:"Source_StreamSize_String,omitempty"`
	SourceStreamSizeString1        string  `json:"Source_StreamSize_String1,omitempty"`
	SourceStreamSizeString2        string  `json:"Source_StreamSize_String2,omitempty"`
	SourceStreamSizeString3        string  `json:"Source_StreamSize_String3,omitempty"`
	SourceStreamSizeString4        string  `json:"Source_StreamSize_String4,omitempty"`
	SourceStreamSizeString5        string  `json:"Source_StreamSize_String5,omitempty"`
	SourceStreamSizeProportion     string  `json:"Source_StreamSize_Proportion,omitempty"`
	// 2-char
	Language        string `json:"Language,omitempty"`
	LanguageString  string `json:"Language_String,omitempty"`
	LanguageString1 string `json:"Language_String1,omitempty"`
	LanguageString2 string `json:"Language_String2,omitempty"`
	// 3-char
	LanguageString3       string `json:"Language_String3,omitempty"`
	LanguageString4       string `json:"Language_String4,omitempty"`
	TimeCodeFirstFrame    string `json:"TimeCode_FirstFrame,omitempty"`
	TimeCodeStriped       string `json:"TimeCode_Striped,omitempty"`
	TimeCodeStripedString string `json:"TimeCode_Striped_String,omitempty"`
}

func (m *MediaInfo) GetContainer() *MediaInfoTrack {
	return m.GetContainerInfoTrack()
}

func (m *MediaInfo) IsCaptionSidecar() bool {
	container := m.GetContainerInfoTrack()
	if container == nil {
		return false
	}
	format := strings.ToUpper(container.Format)
	if format == "TTML" || format == "WEBVTT" || format == "SRT" {
		return true
	}

	return false
}

var trackIDRegex = regexp.MustCompile(`(\d+)`)

func (mt *MediaInfoTrack) GetIDNumeric() int {
	if mt.ID == "" {
		return 0
	}
	match := trackIDRegex.FindStringSubmatch(mt.ID)
	if len(match) == 0 {
		return 0
	}
	id, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}

	return id
}

func (mt *MediaInfoTrack) GetType() TrackType {
	switch mt.TrackTypeMixedCase {
	case MediaInfoTrackTypeGeneral:
		return TrackTypeGeneral
	case MediaInfoTrackTypeVideo:
		return TrackTypeVideo
	case MediaInfoTrackTypeAudio:
		return TrackTypeAudio
	case MediaInfoTrackTypeText:
		return TrackTypeCaption
	case MediaInfoTrackTypeImage:
		return TrackTypeImage
	case MediaInfoTrackTypeMenu:
		return TrackTypeMenu
	default:
		return ""
	}
}

func (m *MediaInfo) GetVideoTracks() []*MediaInfoTrack {
	return m.getTracksByType(TrackTypeVideo)
}

func (m *MediaInfo) GetAudioTracks() []*MediaInfoTrack {
	return m.getTracksByType(TrackTypeAudio)
}

func (m *MediaInfo) GetCaptionTracks() []*MediaInfoTrack {
	return m.getTracksByType(TrackTypeCaption)
}

// Not really a track, but it's described by mediainfo as such
func (m *MediaInfo) GetContainerInfoTrack() *MediaInfoTrack {
	for i := 0; i < len(m.Media.Tracks); i++ {
		t := m.Media.Tracks[i]
		if t.GetType() == TrackTypeGeneral {
			return t
		}
	}

	return nil
}

func (m *MediaInfo) getTracksByType(tt TrackType) []*MediaInfoTrack {
	var tracks []*MediaInfoTrack
	for i := 0; i < len(m.Media.Tracks); i++ {
		t := m.Media.Tracks[i]
		if t.GetType() == tt {
			tracks = append(tracks, t)
		}
	}

	return tracks
}

func (m *MediaInfo) GetVideoTrackByID(id int) *MediaInfoTrack {
	return m.getTrackByTypeAndID(TrackTypeVideo, id)
}

func (m *MediaInfo) GetAudioTrackByID(id int) *MediaInfoTrack {
	return m.getTrackByTypeAndID(TrackTypeAudio, id)
}

func (m *MediaInfo) GetCaptionTrackByID(id int) *MediaInfoTrack {
	return m.getTrackByTypeAndID(TrackTypeCaption, id)
}

func (m *MediaInfo) getTrackByTypeAndID(tt TrackType, id int) *MediaInfoTrack {
	for i := 0; i < len(m.Media.Tracks); i++ {
		t := m.Media.Tracks[i]
		if t.GetType() == tt && t.GetIDNumeric() == id {
			return t
		}
	}

	return nil
}
