package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/live-server/stream"
)

type Publisher struct {
	stream.SourceImpl
	deMuxer libflv.DeMuxer
}

func NewPublisher(sourceId string) *Publisher {
	publisher := &Publisher{SourceImpl: stream.SourceImpl{Id_: sourceId}}
	muxer := &libflv.DeMuxer{}
	muxer.SetHandler(publisher)
	return publisher
}

func (p *Publisher) OnVideo(data []byte, ts uint32) {
	_ = p.deMuxer.InputVideo(data, ts)
}

func (p *Publisher) OnAudio(data []byte, ts uint32) {
	_ = p.deMuxer.InputAudio(data, ts)
}
