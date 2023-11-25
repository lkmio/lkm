package rtmp

import (
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
	"net"
)

func NewSink(id stream.SinkId, conn net.Conn) stream.ISink {
	return &stream.SinkImpl{Id_: id, Protocol_: stream.ProtocolRtmp, Conn: conn, DesiredAudioCodecId_: utils.AVCodecIdNONE, DesiredVideoCodecId_: utils.AVCodecIdNONE}
}
