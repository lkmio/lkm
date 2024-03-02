package hls

import (
	"github.com/yangjiechina/avformat/libmpeg"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

type Stream struct {
	stream.TransStreamImpl
	muxer libmpeg.TSMuxer
}

func NewTransStream(segmentDuration, playlistLength int) stream.ITransStream {
	return &Stream{muxer: libmpeg.NewTSMuxer()}
}

func (t *Stream) Input(packet utils.AVPacket) {
	if utils.AVMediaTypeVideo == packet.MediaType() {
		if packet.KeyFrame() {
			t.Tracks[packet.Index()].AnnexBExtraData()
			t.muxer.Input()
		}
	}

}

func (t *Stream) AddTrack(stream utils.AVStream) {
	t.TransStreamImpl.AddTrack(stream)

	t.muxer.AddTrack(stream.Type(), stream.CodecId())
}

func (t *Stream) WriteHeader() error {
	t.muxer.WriteHeader()
	return nil
}
