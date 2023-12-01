package rtmp

import (
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
	"net"
)

// Session 负责除连接和断开以外的所有RTMP生命周期处理
type Session interface {
	Input(conn net.Conn, data []byte) error //接受网络数据包再交由Stack处理

	Close()
}

func NewSession(conn net.Conn) Session {
	impl := &sessionImpl{}
	impl.Protocol = stream.ProtocolRtmpStr
	impl.RemoteAddr = conn.RemoteAddr().String()

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

	isPublish bool
	conn      net.Conn
}

func (s *sessionImpl) OnPublish(app, stream_ string, response chan utils.HookState) {
	s.SessionImpl.Stream = app + "/" + stream_
	publisher := NewPublisher(s.SessionImpl.Stream, s.stack)
	s.stack.SetOnPublishHandler(publisher)
	s.stack.SetOnTransDeMuxerHandler(publisher)

	//stream.SessionImpl统一处理, Source是否已经存在, Hook回调....
	s.SessionImpl.OnPublish(publisher, nil, func() {
		s.handle = publisher
		s.isPublish = true
		publisher.Init()

		response <- utils.HookStateOK
	}, func(state utils.HookState) {
		response <- state
	})
}

func (s *sessionImpl) OnPlay(app, stream_ string, response chan utils.HookState) {
	s.SessionImpl.Stream = app + "/" + stream_

	sink := NewSink(stream.GenerateSinkId(s.conn), s.SessionImpl.Stream, s.conn)
	s.SessionImpl.OnPlay(sink, nil, func() {
		s.handle = sink
		response <- utils.HookStateOK
	}, func(state utils.HookState) {
		response <- state
	})
}

func (s *sessionImpl) Input(conn net.Conn, data []byte) error {
	//如果是推流，并且握手成功，后续收到的包，都将发送给LoopEvent处理
	if s.isPublish {
		s.handle.(*Publisher).AddEvent(stream.SourceEventInput, data)
		return nil
	} else {
		return s.stack.Input(conn, data)
	}
}

func (s *sessionImpl) Close() {
	if s.handle == nil {
		return
	}

	_, ok := s.handle.(*Publisher)
	if ok {
		if s.isPublish {
			s.handle.(*Publisher).AddEvent(stream.SourceEventClose, nil)
		}
	} else {
		sink := s.handle.(stream.ISink)
		sink.Close()
	}
}
