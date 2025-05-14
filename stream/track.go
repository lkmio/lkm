package stream

import (
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
)

type Track struct {
	Stream        *avformat.AVStream
	Pts           int64 // 最新的PTS
	Dts           int64 // 最新的DTS
	FrameDuration int   // 单帧时长, timebase和推流一致
	Packets       collections.LinkedList[*collections.ReferenceCounter[*avformat.AVPacket]]
}

func NewTrack(stream *avformat.AVStream, dts, pts int64) *Track {
	return &Track{stream, dts, pts, 0, collections.LinkedList[*collections.ReferenceCounter[*avformat.AVPacket]]{}}
}
