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

func (s *Sink) Start() {
	_ = s.stack.SendStreamBeginChunk(s.Conn)
}

func (s *Sink) Flush() {
	_ = s.stack.SendStreamEOFChunk(s.Conn)
}

func (s *Sink) Close() {
	s.stack = nil
	s.BaseSink.Close()
}

func NewSink(id stream.SinkId, sourceId string, conn net.Conn, stack *librtmp.Stack) stream.Sink {
	return &Sink{
		BaseSink: stream.BaseSink{Id_: id, SourceId_: sourceId, State_: stream.SessionStateCreate, Protocol_: stream.ProtocolRtmp, Conn: conn, DesiredAudioCodecId_: utils.AVCodecIdNONE, DesiredVideoCodecId_: utils.AVCodecIdNONE},
		stack:    stack,
	}
}
