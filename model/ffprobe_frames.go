package model

import "strconv"

type FfprobeFrames struct {
	Frames []VideoFrame `json:"frames"`
}

type VideoFrame struct {
	MediaType               string `json:"media_type"`
	StreamIndex             int    `json:"stream_index"`
	KeyFrame                int    `json:"key_frame"`
	Pts                     int    `json:"pts"`
	PtsTime                 string `json:"pts_time"`
	PktDts                  int    `json:"pkt_dts"`
	PktDtsTime              string `json:"pkt_dts_time"`
	BestEffortTimestamp     int    `json:"best_effort_timestamp"`
	BestEffortTimestampTime string `json:"best_effort_timestamp_time"`
	PktDuration             int    `json:"pkt_duration"`
	PktDurationTime         string `json:"pkt_duration_time"`
	Duration                int    `json:"duration"`
	DurationTime            string `json:"duration_time"`
	PktPos                  string `json:"pkt_pos"`
	PktSize                 string `json:"pkt_size"`
	Width                   int    `json:"width"`
	Height                  int    `json:"height"`
	PixFmt                  string `json:"pix_fmt"`
	SampleAspectRatio       string `json:"sample_aspect_ratio"`
	PictType                string `json:"pict_type"`
	CodedPictureNumber      int    `json:"coded_picture_number"`
	DisplayPictureNumber    int    `json:"display_picture_number"`
	InterlacedFrame         int    `json:"interlaced_frame"`
	TopFieldFirst           int    `json:"top_field_first"`
	RepeatPict              int    `json:"repeat_pict"`
	ChromaLocation          string `json:"chroma_location"`
	SideDataList            []struct {
		SideDataType string `json:"side_data_type"`
	} `json:"side_data_list"`
	Tags struct {
		SceneScore string `json:"lavfi.scene_score"`
	} `json:"tags"`
}

func (f *VideoFrame) GetSceneScore() float64 {
	val, _ := strconv.ParseFloat(f.Tags.SceneScore, 64)

	return val
}

func (f *VideoFrame) GetPtsTimeFloat64() float64 {
	val, _ := strconv.ParseFloat(f.PtsTime, 64)

	return val
}
