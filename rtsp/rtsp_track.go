package rtsp

import (
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
	Muxer     rtp.Muxer
}

func (r *Track) Close() {

}

func NewRTSPTrack(muxer rtp.Muxer, payload rtp.PayloadType, mediaType utils.AVMediaType, id utils.AVCodecID) *Track {
	stream := &Track{
		payload:   payload,
		MediaType: mediaType,
		CodecID:   id,
		Muxer:     muxer,
	}

	return stream
}
