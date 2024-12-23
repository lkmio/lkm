package rtmp

import (
	"github.com/lkmio/avformat/librtmp"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/stream"
	"net"
)

type Sink struct {
	stream.BaseSink
	stack *librtmp.Stack
}

func (s *Sink) StartStreaming(_ stream.TransStream) error {
	return s.stack.SendStreamBeginChunk()
}

func (s *Sink) StopStreaming(stream stream.TransStream) {
	_ = s.stack.SendStreamEOFChunk()
	s.BaseSink.StopStreaming(stream)
}

func (s *Sink) Close() {
	s.stack = nil
	s.BaseSink.Close()
}

func NewSink(id stream.SinkID, sourceId string, conn net.Conn, stack *librtmp.Stack) stream.Sink {
	return &Sink{
		BaseSink: stream.BaseSink{ID: id, SourceID: sourceId, State: stream.SessionStateCreated, Protocol: stream.TransStreamRtmp, Conn: conn, DesiredAudioCodecId_: utils.AVCodecIdNONE, DesiredVideoCodecId_: utils.AVCodecIdNONE, TCPStreaming: true},
		stack:    stack,
	}
}
