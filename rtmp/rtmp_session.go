package rtmp

import (
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/log"
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

	stack := librtmp.NewStack(impl)
	impl.stack = stack
	impl.conn = conn
	return impl
}

type sessionImpl struct {
	//解析rtmp协议栈
	stack *librtmp.Stack
	//Publisher/sink, 在publish或play成功后赋值
	handle      interface{}
	isPublisher bool

	conn net.Conn
}

func (s *sessionImpl) OnPublish(app, stream_ string, response chan utils.HookState) {
	log.Sugar.Infof("rtmp onpublish app:%s stream:%s conn:%s", app, stream_, s.conn.RemoteAddr().String())

	sourceId := app + "_" + stream_
	source := NewPublisher(sourceId, s.stack, s.conn)
	//设置推流的音视频回调
	s.stack.SetOnPublishHandler(source)

	//推流事件Source统一处理, 是否已经存在, Hook回调....
	source.Publish(source, func() {
		s.handle = source
		s.isPublisher = true

		source.Init(source.Input)
		go source.LoopEvent()

		response <- utils.HookStateOK
	}, func(state utils.HookState) {
		response <- state
	})
}

func (s *sessionImpl) OnPlay(app, stream_ string, response chan utils.HookState) {
	sourceId := app + "_" + stream_
	//拉流事件Sink统一处理
	sink := NewSink(stream.GenerateSinkId(s.conn.RemoteAddr()), sourceId, s.conn)

	log.Sugar.Infof("rtmp onplay app:%s stream:%s sink:%v conn:%s", app, stream_, sink.Id(), s.conn.RemoteAddr().String())

	stream.HookPlaying(sink, func() {
		s.handle = sink
		response <- utils.HookStateOK
	}, func(state utils.HookState) {
		response <- state
	})
}

func (s *sessionImpl) Input(conn net.Conn, data []byte) error {
	//如果是推流，并且握手成功，后续收到的包，都将发送给LoopEvent处理
	if s.isPublisher {
		s.handle.(*Publisher).AddEvent(stream.SourceEventInput, data)
		return nil
	} else {
		return s.stack.Input(conn, data)
	}
}

func (s *sessionImpl) Close() {
	log.Sugar.Debugf("释放rtmp session conn:%s", s.conn.RemoteAddr().String())

	//释放协议栈
	if s.stack != nil {
		s.stack.Close()
	}

	//还没到publish/play
	if s.handle == nil {
		return
	}

	_, ok := s.handle.(*Publisher)
	if ok {
		if s.isPublisher {
			s.handle.(*Publisher).AddEvent(stream.SourceEventClose, nil)
		}
	} else {
		sink := s.handle.(stream.ISink)
		sink.Close()
	}
}
