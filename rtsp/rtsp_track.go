package rtsp

import (
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/rtp"
)

// Track rtsp每路输出流的封装
type Track struct {
	PT        byte
	Rate      int
	MediaType utils.AVMediaType
	StartSeq  uint16
	EndSeq    uint16

	Muxer           rtp.Muxer
	ExtraDataBuffer []*collections.ReferenceCounter[[]byte] // 缓存带有编码信息的rtp包, 对所有sink通用
}

func (r *Track) Close() {
}

func NewRTSPTrack(muxer rtp.Muxer, pt byte, rate int, mediaType utils.AVMediaType) *Track {
	stream := &Track{
		PT:        pt,
		Rate:      rate,
		Muxer:     muxer,
		MediaType: mediaType,
	}

	return stream
}
