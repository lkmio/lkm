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
	//设置回调，从flv解析出来的Stream和AVPacket都将统一回调到stream.SourceImpl
	muxer.SetHandler(publisher)
	return publisher
}

// OnVideo 从rtmpchunk解析过来的视频包
func (p *Publisher) OnVideo(data []byte, ts uint32) {
	_ = p.deMuxer.InputVideo(data, ts)
}

func (p *Publisher) OnAudio(data []byte, ts uint32) {
	_ = p.deMuxer.InputAudio(data, ts)
}
