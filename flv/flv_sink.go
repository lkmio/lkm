package flv

import (
	"github.com/lkmio/lkm/stream"
	"net"
)

func NewFLVSink(id stream.SinkId, sourceId string, conn net.Conn) stream.Sink {
	return &stream.BaseSink{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolFlv, Conn: conn}
}
