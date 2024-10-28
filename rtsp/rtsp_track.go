package rtsp

import (
	"github.com/lkmio/avformat/librtp"
	"github.com/lkmio/avformat/utils"
)

// Track RtspTrack 对rtsp每路输出流的封装
type Track struct {
	pt        byte
	rate      int
	mediaType utils.AVMediaType
	seq       uint16

	muxer           librtp.Muxer
	extraDataBuffer [][]byte // 缓存带有编码信息的rtp包, 对所有sink通用
}

func (r *Track) Close() {
}

func NewRTSPTrack(muxer librtp.Muxer, pt byte, rate int, mediaType utils.AVMediaType) *Track {
	stream := &Track{
		pt:        pt,
		rate:      rate,
		muxer:     muxer,
		mediaType: mediaType,
	}

	return stream
}
