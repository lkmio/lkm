package stream

import "github.com/yangjiechina/avformat/utils"

// OnTransDeMuxerHandler 解复用器回调 /**
type OnTransDeMuxerHandler interface {
	OnDeMuxStream(stream utils.AVStream)
	OnDeMuxStreamDone()
	OnDeMuxPacket(index int, packet utils.AVPacket)
	OnDeMuxDone()
}

type ITransDeMuxer interface {
	Input(data []byte)

	SetHandler(handler OnTransDeMuxerHandler)

	Close()
}

type TransDeMuxerImpl struct {
	handler OnTransDeMuxerHandler
}

func (impl *TransDeMuxerImpl) Input(data []byte) {
	panic("implement me")
}

func (impl *TransDeMuxerImpl) SetHandler(handler OnTransDeMuxerHandler) {
	impl.handler = handler
}

func (impl *TransDeMuxerImpl) Close() {
	panic("implement me")
}
