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
	return &sink{stream.SinkImpl{Id_: id, SourceId_: sourceId}, w}
}

func (s *sink) Input(data []byte) error {
	if s.conn != nil {
		_, err := s.conn.Write(data)

		return err
	}

	return nil
}
