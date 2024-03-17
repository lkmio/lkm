package rtsp

import (
	"github.com/yangjiechina/avformat/librtp"
	"github.com/yangjiechina/avformat/utils"
)

type rtpTrack struct {
	pt        byte
	rate      int
	mediaType utils.AVMediaType

	//目前用于缓存带有SPS和PPS的RTP包
	buffer []byte
	muxer  librtp.Muxer
	cache  bool

	header [][]byte
	tmp    [][]byte
}

func NewRTPTrack(muxer librtp.Muxer, pt byte, rate int) *rtpTrack {
	stream := &rtpTrack{
		pt:     pt,
		rate:   rate,
		muxer:  muxer,
		buffer: make([]byte, 1500),
	}

	return stream
}
