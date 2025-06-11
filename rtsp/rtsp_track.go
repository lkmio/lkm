package rtsp

import (
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/rtp"
)

// Track rtsp每路输出流的封装
type Track struct {
	payload   rtp.PayloadType
	MediaType utils.AVMediaType
	StartSeq  uint16
	EndSeq    uint16
	CodecID   utils.AVCodecID

	Muxer           rtp.Muxer
	ExtraDataBuffer []*collections.ReferenceCounter[[]byte] // 缓存带有编码信息的rtp包, 对所有sink通用
}

func (r *Track) Close() {
}

func NewRTSPTrack(muxer rtp.Muxer, payload rtp.PayloadType, mediaType utils.AVMediaType, id utils.AVCodecID) *Track {
	stream := &Track{
		payload:   payload,
		Muxer:     muxer,
		MediaType: mediaType,
		CodecID:   id,
	}

	return stream
}
