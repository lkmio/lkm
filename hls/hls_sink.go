package hls

import (
	"github.com/yangjiechina/live-server/stream"
	"net/http"
)

type sink struct {
	stream.SinkImpl
	conn http.ResponseWriter
}

func NewSink(id stream.SinkId, sourceId string, w http.ResponseWriter) stream.ISink {
	return &sink{stream.SinkImpl{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolHls}, w}
}

func (s *sink) Input(data []byte) error {
	if s.conn != nil {
		_, err := s.conn.Write(data)

		return err
	}

	return nil
}

type m3u8Sink struct {
	stream.SinkImpl
}

func (s *m3u8Sink) Input(data []byte) error {

	return nil
}

func NewM3U8Sink(id stream.SinkId, sourceId string, w http.ResponseWriter) stream.ISink {
	return &m3u8Sink{stream.SinkImpl{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolHls}}
}
