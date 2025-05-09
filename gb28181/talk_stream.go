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

func (s *TalkStream) Input(packet *avformat.AVPacket) ([]*collections.ReferenceCounter[[]byte], int64, bool, error) {
	var size int
	s.muxer.Input(packet.Data, uint32(packet.Dts), func() []byte {
		return s.packet
	}, func(pkt []byte) {
		size = len(pkt)
	})

	packet = &avformat.AVPacket{Data: s.packet[:size]}
	return s.RtpStream.Input(packet)
}

func NewTalkTransStream() (stream.TransStream, error) {
	return &TalkStream{
		RtpStream: stream.NewRtpTransStream(stream.TransStreamGBTalkForward, 1024),
		muxer:     rtp.NewMuxer(8, 0, 0xFFFFFFFF),
		packet:    make([]byte, 1500),
	}, nil
}

func TalkTransStreamFactory(source stream.Source, protocol stream.TransStreamProtocol, tracks []*stream.Track) (stream.TransStream, error) {
	return NewTalkTransStream()
}
