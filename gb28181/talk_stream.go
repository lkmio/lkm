package gb28181

import (
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/rtp"
)

type TalkStream struct {
	*stream.RtpStream
	muxer  rtp.Muxer
	packet []byte
}

func (s *TalkStream) WriteHeader() error {
	return nil
}

func (s *TalkStream) Input(packet *avformat.AVPacket, index int) ([]*collections.ReferenceCounter[[]byte], int64, bool, error) {
	var size int
	s.muxer.Input(packet.Data, uint32(packet.Dts), func() []byte {
		return s.packet
	}, func(pkt []byte) {
		size = len(pkt)
	})

	packet = &avformat.AVPacket{Data: s.packet[:size]}
	return s.RtpStream.Input(packet, index)
}

func NewTalkTransStream(ssrc uint32) (stream.TransStream, error) {
	return &TalkStream{
		RtpStream: stream.NewRtpTransStream(stream.TransStreamGBTalk, 1024),
		muxer:     rtp.NewMuxer(8, 0, ssrc),
		packet:    make([]byte, 1500),
	}, nil
}

func TalkTransStreamFactory(_ stream.Source, _ stream.TransStreamProtocol, _ []*stream.Track, sink stream.Sink) (stream.TransStream, error) {
	var ssrc uint32 = 0xFFFFFFFF
	if sink != nil {
		forwardSink, ok := sink.(*stream.ForwardSink)
		if ok {
			ssrc = forwardSink.GetSSRC()
		}
	}

	return NewTalkTransStream(ssrc)
}
