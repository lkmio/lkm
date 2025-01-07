package flv

import (
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/lkm/stream"
	"net"
)

type Sink struct {
	stream.BaseSink
	prevTagSize uint32
}

func (s *Sink) StopStreaming(stream stream.TransStream) {
	s.BaseSink.StopStreaming(stream)
	s.prevTagSize = stream.(*TransStream).Muxer.PrevTagSize()
}

func (s *Sink) Write(index int, data [][]byte, ts int64) error {
	// 恢复推流时, 不发送9个字节的flv header
	if s.prevTagSize > 0 {
		data = data[1:]
		s.prevTagSize = 0
	}

	return s.BaseSink.Write(index, data, ts)
}

func NewFLVSink(id stream.SinkID, sourceId string, conn net.Conn) stream.Sink {
	return &Sink{BaseSink: stream.BaseSink{ID: id, SourceID: sourceId, State: stream.SessionStateCreated, Protocol: stream.TransStreamFlv, Conn: transport.NewConn(conn), TCPStreaming: true}}
}
