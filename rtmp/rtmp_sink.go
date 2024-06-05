package rtmp

import (
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/stream"
	"net"
)

func NewSink(id stream.SinkId, sourceId string, conn net.Conn) stream.ISink {
	return &stream.SinkImpl{Id_: id, SourceId_: sourceId, State_: stream.SessionStateCreate, Protocol_: stream.ProtocolRtmp, Conn: conn, DesiredAudioCodecId_: utils.AVCodecIdNONE, DesiredVideoCodecId_: utils.AVCodecIdNONE}
}
