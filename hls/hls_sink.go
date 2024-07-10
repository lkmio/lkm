package hls

import (
	"github.com/lkmio/lkm/stream"
)

type tsSink struct {
	stream.BaseSink
}

func NewTSSink(id stream.SinkId, sourceId string) stream.Sink {
	return &tsSink{stream.BaseSink{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolHls}}
}

func (s *tsSink) Input(data []byte) error {
	return nil
}

type m3u8Sink struct {
	stream.BaseSink
	cb func(m3u8 []byte)
}

func (s *m3u8Sink) Input(data []byte) error {
	s.cb(data)
	return nil
}

func NewM3U8Sink(id stream.SinkId, sourceId string, cb func(m3u8 []byte)) stream.Sink {
	return &m3u8Sink{stream.BaseSink{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolHls}, cb}
}
