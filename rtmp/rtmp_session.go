package rtmp

import (
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
	"net"
)

// Session 负责除RTMP连接和断开以外的所有生命周期处理
type Session interface {
	Input(conn net.Conn, data []byte) error //接受网络数据包再交由Stack处理

	Close()
}

func NewSession(conn net.Conn) Session {
	impl := &sessionImpl{}
	stack := librtmp.NewStack(impl)
	impl.stack = stack
	impl.conn = conn
	return impl
}

type sessionImpl struct {
	stream.SessionImpl
	//解析rtmp协议栈
	stack *librtmp.Stack
	//publisher/sink
	handle interface{}
	conn   net.Conn

	streamId string
}

func (s *sessionImpl) OnPublish(app, stream_ string, response chan utils.HookState) {
	s.streamId = app + "/" + stream_
	publisher := NewPublisher(s.streamId)
	s.stack.SetOnPublishHandler(publisher)
	s.stack.SetOnTransDeMuxerHandler(publisher)
	//stream.SessionImpl统一处理, Source是否已经存在, Hook回调....
	s.SessionImpl.OnPublish(publisher, nil, func() {
		s.handle = publisher
		response <- utils.HookStateOK
	}, func(state utils.HookState) {
		response <- state
	})
}

func (s *sessionImpl) OnPlay(app, stream_ string, response chan utils.HookState) {
	s.streamId = app + "/" + stream_

	sink := NewSink(stream.GenerateSinkId(s.conn), s.conn)
	s.SessionImpl.OnPlay(sink, nil, func() {
		s.handle = sink
		response <- utils.HookStateOK
	}, func(state utils.HookState) {
		response <- state
	})
}

func (s *sessionImpl) Input(conn net.Conn, data []byte) error {
	return s.stack.Input(conn, data)
}

func (s *sessionImpl) Close() {
}
