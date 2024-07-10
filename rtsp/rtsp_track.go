package rtsp

import (
	"github.com/lkmio/avformat/librtp"
	"github.com/lkmio/avformat/utils"
)

// 对rtsp每路输出流的封装
type rtspTrack struct {
	pt        byte
	rate      int
	mediaType utils.AVMediaType

	buffer []byte //buffer of rtp packet
	muxer  librtp.Muxer
	cache  bool

	extraDataBuffer    [][]byte //缓存带有编码信息的rtp包, 对所有sink通用
	tmpExtraDataBuffer [][]byte //缓存带有编码信息的rtp包, 整个过程会多次回调(sps->pps->sei...), 先保存到临时区, 最后再缓存到extraDataBuffer
}

func (r *rtspTrack) Close() {
	if r.muxer != nil {
		r.muxer.Close()
		r.muxer = nil
	}
}

func NewRTSPTrack(muxer librtp.Muxer, pt byte, rate int, mediaType utils.AVMediaType) *rtspTrack {
	stream := &rtspTrack{
		pt:        pt,
		rate:      rate,
		muxer:     muxer,
		buffer:    make([]byte, 1500),
		mediaType: mediaType,
	}

	return stream
}
