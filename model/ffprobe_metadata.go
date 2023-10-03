package model

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/terranvigil/dynamic-crf/maths"
)

type FfprobeMetadata struct {
	Streams []FfprobeStream `json:"streams"`
	Format  FfprobeFormat   `json:"format"`
}
type FfprobeStream struct {
	Index              int               `json:"index"`
	CodecName          string            `json:"codec_name"`
	CodecLongName      string            `json:"codec_long_name"`
	Profile            string            `json:"profile"`
	CodecType          string            `json:"codec_type"`
	CodecTagString     string            `json:"codec_tag_string"`
	CodecTag           string            `json:"codec_tag"`
	Width              int               `json:"width,omitempty"`
	Height             int               `json:"height,omitempty"`
	CodedWidth         int               `json:"coded_width,omitempty"`
	CodedHeight        int               `json:"coded_height,omitempty"`
	ClosedCaptions     int               `json:"closed_captions,omitempty"`
	FilmGrain          int               `json:"film_grain,omitempty"`
	HasBFrames         int               `json:"has_b_frames,omitempty"`
	SampleAspectRatio  string            `json:"sample_aspect_ratio,omitempty"`
	DisplayAspectRatio string            `json:"display_aspect_ratio,omitempty"`
	PixFmt             string            `json:"pix_fmt,omitempty"`
	Level              int               `json:"level,omitempty"`
	ColorRange         string            `json:"color_range,omitempty"`
	ColorSpace         string            `json:"color_space,omitempty"`
	ColorTransfer      string            `json:"color_transfer,omitempty"`
	ColorPrimaries     string            `json:"color_primaries,omitempty"`
	ChromaLocation     string            `json:"chroma_location,omitempty"`
	FieldOrder         string            `json:"field_order,omitempty"`
	Refs               int               `json:"refs,omitempty"`
	IsAvc              string            `json:"is_avc,omitempty"`
	NalLengthSize      string            `json:"nal_length_size,omitempty"`
	ID                 string            `json:"id"`
	RFrameRate         string            `json:"r_frame_rate"`
	AvgFrameRate       string            `json:"avg_frame_rate"`
	TimeBase           string            `json:"time_base"`
	StartPts           int               `json:"start_pts"`
	StartTime          string            `json:"start_time"`
	DurationTS         int               `json:"duration_ts"`
	Duration           string            `json:"duration"`
	BitRate            string            `json:"bit_rate"`
	BitsPerRawSample   string            `json:"bits_per_raw_sample,omitempty"`
	NbFrames           string            `json:"nb_frames"`
	ExtradataSize      int               `json:"extradata_size"`
	Disposition        FfprobeDispostion `json:"disposition"`
	Tags               FfprobeStreamTag  `json:"tags"`
	SampleFmt          string            `json:"sample_fmt,omitempty"`
	SampleRate         string            `json:"sample_rate,omitempty"`
	Channels           int               `json:"channels,omitempty"`
	ChannelLayout      string            `json:"channel_layout,omitempty"`
	BitsPerSample      int               `json:"bits_per_sample,omitempty"`
}

func (s *FfprobeStream) GetID() int {
	if id, err := maths.HexToInt(s.ID); err != nil {
		return -1
	} else {
		return int(id)
	}
}

type FfprobeDispostion struct {
	Default         int `json:"default"`
	Dub             int `json:"dub"`
	Original        int `json:"original"`
	Comment         int `json:"comment"`
	Lyrics          int `json:"lyrics"`
	Karaoke         int `json:"karaoke"`
	Forced          int `json:"forced"`
	HearingImpaired int `json:"hearing_impaired"`
	VisualImpaired  int `json:"visual_impaired"`
	CleanEffects    int `json:"clean_effects"`
	AttachedPic     int `json:"attached_pic"`
	TimedThumbnails int `json:"timed_thumbnails"`
	Captions        int `json:"captions"`
	Descriptions    int `json:"descriptions"`
	Metadata        int `json:"metadata"`
	Dependent       int `json:"dependent"`
	StillImage      int `json:"still_image"`
}

type FfprobeStreamTag struct {
	CreationTime time.Time `json:"creation_time"`
	Language     string    `json:"language"`
	HandlerName  string    `json:"handler_name"`
	VendorID     string    `json:"vendor_id"`
}

type FfprobeFormat struct {
	Filename       string           `json:"filename"`
	NbStreams      int              `json:"nb_streams"`
	NbPrograms     int              `json:"nb_programs"`
	FormatName     string           `json:"format_name"`
	FormatLongName string           `json:"format_long_name"`
	StartTime      string           `json:"start_time"`
	Duration       string           `json:"duration"`
	Size           string           `json:"size"`
	BitRate        string           `json:"bit_rate"`
	ProbeScore     int              `json:"probe_score"`
	Tags           FfprobeFormatTag `json:"tags"`
}

type FfprobeFormatTag struct {
	MajorBrand       string    `json:"major_brand"`
	MinorVersion     string    `json:"minor_version"`
	CompatibleBrands string    `json:"compatible_brands"`
	CreationTime     time.Time `json:"creation_time"`
	Encoder          string    `json:"encoder"`
}

func (f *FfprobeMetadata) StreamByID(id int) *FfprobeStream {
	for i := 0; i < len(f.Streams); i++ {
		s := f.Streams[i]
		if s.GetID() == id {
			return &s
		}
	}
	return nil
}

func (f *FfprobeMetadata) VideoStream() *FfprobeStream {
	for i := 0; i < len(f.Streams); i++ {
		s := f.Streams[i]
		if s.CodecType == "video" {
			return &s
		}
	}
	return nil
}

func (f *FfprobeMetadata) AudioStream() *FfprobeStream {
	for i := 0; i < len(f.Streams); i++ {
		s := f.Streams[i]
		if s.CodecType == "audio" {
			return &s
		}
	}
	return nil
}

func (s *FfprobeStream) BitRateInt() (int, error) {
	return strconv.Atoi(s.BitRate)
}

func FractionToFloat(val string) (float64, error) {
	parts := strings.Split(val, "/")
	if len(parts) != 2 {
		return 0, errors.New("invalid fraction: " + val)
	}
	numerator, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, errors.New("invalid numerator: " + val)
	}
	denominator, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, errors.New("invalid denominator: " + val)
	}

	return float64(numerator) / float64(denominator), nil
}
