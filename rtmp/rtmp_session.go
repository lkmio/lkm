package rtmp

import (
	"github.com/yangjiechina/avformat"
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/live-server/stream"
	"net"
	"net/http"
)

// Session 负责除RTMP连接和断开以外的所有生命周期处理
type Session interface {
	Input(conn net.Conn, data []byte) error //接受网络数据包再交由Stack处理

	Close()
}

func NewSession() *sessionImpl {
	impl := &sessionImpl{}
	stack := librtmp.NewStack(impl)
	impl.stack = stack
	return impl
}

type sessionImpl struct {
	stream.SessionImpl
	stack *librtmp.Stack
	//publisher/sink
	handle interface{}

	streamId string
}

func (s *sessionImpl) OnPublish(app, stream_ string, response chan avformat.HookState) {
	s.streamId = app + "/" + stream_
	publisher := NewPublisher(s.streamId)
	s.stack.SetOnPublishHandler(publisher)
	s.SessionImpl.OnPublish(publisher, nil, func() {
		s.handle = publisher
		response <- http.StatusOK
	}, func(state avformat.HookState) {
		response <- state
	})
}

func (s *sessionImpl) OnPlay(app, stream string, response chan avformat.HookState) {
	s.streamId = app + "/" + stream
	//sink := &Sink{}
	//s.SessionImpl.OnPlay(sink, nil, func() {
	//	s.handle = sink
	//	response <- http.StatusOK
	//}, func(state avformat.HookState) {
	//	response <- state
	//})
}

func (s *sessionImpl) Input(conn net.Conn, data []byte) error {
	return s.stack.Input(conn, data)
}

func (s *sessionImpl) Close() {
}
