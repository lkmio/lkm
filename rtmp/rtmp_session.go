package rtmp

import (
	"github.com/lkmio/avformat/librtmp"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"net"
)

// Session RTMP会话, 解析处理Message
type Session struct {
	stack       *librtmp.Stack // rtmp协议栈, 解析message
	handle      interface{}    // 持有具体会话句柄(推流端/拉流端)， 在@see OnPublish @see OnPlay回调中赋值
	isPublisher bool           // 是否时推流会话

	conn          net.Conn
	receiveBuffer *stream.ReceiveBuffer // 推流源收流队列
}

func (s *Session) generateSourceID(app, stream string) string {
	if len(app) == 0 {
		return stream
	} else if len(stream) == 0 {
		return app
	} else {
		return app + "/" + stream
	}
}

func (s *Session) OnPublish(app, stream_ string) utils.HookState {
	log.Sugar.Infof("rtmp onpublish app:%s stream:%s conn:%s", app, stream_, s.conn.RemoteAddr().String())

	streamName, values := stream.ParseUrl(stream_)

	sourceId := s.generateSourceID(app, streamName)
	source := NewPublisher(sourceId, s.stack, s.conn)
	// 设置推流的音视频回调
	s.stack.SetOnPublishHandler(source)

	// 初始化放在add source前面, 以防add后再init, 空窗期拉流队列空指针.
	source.Init(stream.ReceiveBufferTCPBlockCount)
	source.SetUrlValues(values)

	// 统一处理source推流事件, source是否已经存在, hook回调....
	_, state := stream.PreparePublishSource(source, true)
	if utils.HookStateOK != state {
		log.Sugar.Errorf("rtmp推流失败 source:%s", sourceId)
	} else {
		s.handle = source
		s.isPublisher = true
		s.receiveBuffer = stream.NewTCPReceiveBuffer()

		go stream.LoopEvent(source)
	}

	return state
}

func (s *Session) OnPlay(app, stream_ string) utils.HookState {
	streamName, values := stream.ParseUrl(stream_)

	sourceId := s.generateSourceID(app, streamName)
	sink := NewSink(stream.NetAddr2SinkId(s.conn.RemoteAddr()), sourceId, s.conn, s.stack)
	sink.SetUrlValues(values)

	log.Sugar.Infof("rtmp onplay app: %s stream: %s sink: %v conn: %s", app, stream_, sink.GetID(), s.conn.RemoteAddr().String())

	_, state := stream.PreparePlaySink(sink)
	if utils.HookStateOK != state {
		log.Sugar.Errorf("rtmp拉流失败 source: %s sink: %s", sourceId, sink.GetID())
	} else {
		s.handle = sink
	}

	return state
}

func (s *Session) Input(conn net.Conn, data []byte) error {
	// 推流会话, 收到的包都将交由主协程处理
	if s.isPublisher {
		s.handle.(*Publisher).PublishSource.Input(data)
		return nil
	} else {
		return s.stack.Input(conn, data)
	}
}

func (s *Session) Close() {
	// session/conn/stack相互引用, go释放不了...手动赋值为nil
	s.conn = nil

	defer func() {
		if s.stack != nil {
			s.stack.Close()
			s.stack = nil
		}
	}()

	// 还未确定会话类型, 无需处理
	if s.handle == nil {
		return
	}

	publisher, ok := s.handle.(*Publisher)
	if ok {
		log.Sugar.Infof("rtmp推流结束 %s", publisher.String())

		if s.isPublisher {
			publisher.Close()
			s.receiveBuffer = nil
		}
	} else {
		sink := s.handle.(*Sink)

		log.Sugar.Infof("rtmp拉流结束 %s", sink.String())
		sink.Close()
	}
}

func NewSession(conn net.Conn) *Session {
	session := &Session{}

	stack := librtmp.NewStack(session)
	session.stack = stack
	session.conn = conn
	return session
}
