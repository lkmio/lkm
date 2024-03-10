package flv

import (
	"github.com/yangjiechina/live-server/stream"
	"net"
)

func NewFLVSink(id stream.SinkId, sourceId string, conn net.Conn) stream.ISink {
	return &stream.SinkImpl{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolFlv, Conn: conn}
}
