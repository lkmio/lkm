package flv

import (
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/lkm/stream"
	"net"
)

func NewFLVSink(id stream.SinkID, sourceId string, conn net.Conn) stream.Sink {
	return &stream.BaseSink{ID: id, SourceID: sourceId, Protocol: stream.TransStreamFlv, Conn: transport.NewConn(conn)}
}
