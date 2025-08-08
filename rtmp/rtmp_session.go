package rtmp

import (
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/rtmp"
	"net"
	"strings"
)

// Session RTMP会话, 解析处理Message
type Session struct {
	conn          net.Conn
	stack         *rtmp.ServerStack // rtmp协议栈, 解析message
	handle        interface{}       // 持有具体会话句柄(推流端/拉流端)， 在@see OnPublish @see OnPlay回调中赋值
	isPublisher   bool              // 是否是推流会话
	receiveBuffer []byte
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
	log.Sugar.Infof("rtmp onpublish app: %s stream: %s conn: %s", app, stream_, s.conn.RemoteAddr().String())

	streamName, values := stream.ParseUrl(stream_)

	sourceId := s.generateSourceID(app, streamName)
	source := NewPublisher(sourceId, s.stack, s.conn)

	// 初始化放在add source前面, 以防add后再init, 空窗期拉流队列空指针.
	source.Init()
	source.SetUrlValues(values)

	// 统一处理source推流事件, source是否已经存在, hook回调....
	state := utils.HookStateOK
	_, err := stream.PreparePublishSource(source, true)
	if err != nil {
		str := err.Error()
		log.Sugar.Errorf("rtmp推流失败 source: %s err: %s", sourceId, str)

		if strings.HasSuffix(str, "exist") {
			state = utils.HookStateOccupy
		} else {
			state = utils.HookStateFailure
		}
	} else {
		s.handle = source
		s.isPublisher = true

		stream.LoopEvent(source)
	}

	return state
}

func (s *Session) OnPlay(app, stream_ string) utils.HookState {
	streamName, values := stream.ParseUrl(stream_)

	sourceId := s.generateSourceID(app, streamName)
	sinkId := stream.NetAddr2SinkID(s.conn.RemoteAddr())
	log.Sugar.Infof("rtmp onplay app: %s stream: %s sink: %v conn: %s", app, stream_, sinkId, s.conn.RemoteAddr().String())

	sink := NewSink(sinkId, sourceId, s.conn, s.stack)
	ok := stream.SubscribeStream(sink, values)
	if utils.HookStateOK != ok {
		log.Sugar.Errorf("rtmp拉流失败 source: %s sink: %s", sourceId, sink.GetID())
	} else {
		s.handle = sink
	}

	return ok
}

func (s *Session) Input(data []byte) error {
	// 推流会话, 收到的包都将交由主协程处理
	if s.isPublisher {
		s.handle.(*Publisher).UpdateReceiveStats(len(data))

		var err error
		s.handle.(*Publisher).ExecuteWithStreamLock(func() {
			err = s.stack.Input(s.conn, data)
		})

		return err
	} else {
		return s.stack.Input(s.conn, data)
	}
}

func (s *Session) Close() {
	// session/conn/stack相互引用, gc回收不了...手动赋值为nil
	s.conn = nil

	defer func() {
		if s.stack != nil {
			s.stack.Close()
			s.stack = nil
		}

		if s.receiveBuffer != nil {
			stream.TCPReceiveBufferPool.Put(s.receiveBuffer[:cap(s.receiveBuffer)])
			s.receiveBuffer = nil
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
		}
	} else {
		sink := s.handle.(*Sink)

		log.Sugar.Infof("rtmp拉流结束 %s", sink.String())
		sink.Close()
	}
}

func NewSession(conn net.Conn) *Session {
	session := &Session{
		receiveBuffer: stream.TCPReceiveBufferPool.Get().([]byte),
	}

	stackServer := rtmp.NewStackServer(false)
	stackServer.SetOnStreamHandler(session)
	session.stack = stackServer
	session.conn = conn
	return session
}
