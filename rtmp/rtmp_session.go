package rtmp

import (
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/log"
	"github.com/yangjiechina/lkm/stream"
	"net"
)

// Session 负责除连接和断开以外的所有RTMP生命周期处理
type Session struct {
	stack       *librtmp.Stack //rtmp协议栈
	handle      interface{}    //Publisher/sink, 在publish或play成功后赋值
	isPublisher bool

	conn          net.Conn
	receiveBuffer *stream.ReceiveBuffer
}

func (s *Session) generateSourceId(app, stream_ string) string {
	if len(app) == 0 {
		return stream_
	} else if len(stream_) == 0 {
		return app
	} else {
		return app + "/" + stream_
	}
}

func (s *Session) OnPublish(app, stream_ string, response chan utils.HookState) {
	log.Sugar.Infof("rtmp onpublish app:%s stream:%s conn:%s", app, stream_, s.conn.RemoteAddr().String())

	streamName, values := stream.ParseUrl(stream_)

	sourceId := s.generateSourceId(app, streamName)
	source := NewPublisher(sourceId, s.stack, s.conn)
	//设置推流的音视频回调
	s.stack.SetOnPublishHandler(source)

	//初始化放在add source前面, 以防add后再init,空窗期拉流队列空指针.
	source.Init(source.Input, source.Close, stream.ReceiveBufferTCPBlockCount)
	source.SetUrlValues(values)

	//推流事件Source统一处理, 是否已经存在, Hook回调....
	_, state := stream.PreparePublishSource(source, true)
	if utils.HookStateOK != state {
		log.Sugar.Errorf("rtmp推流失败 source:%s", sourceId)
	} else {
		s.handle = source
		s.isPublisher = true
		s.receiveBuffer = stream.NewTCPReceiveBuffer()

		go source.LoopEvent()
	}

	response <- state
}

func (s *Session) OnPlay(app, stream_ string, response chan utils.HookState) {
	streamName, values := stream.ParseUrl(stream_)

	sourceId := s.generateSourceId(app, streamName)
	sink := NewSink(stream.GenerateSinkId(s.conn.RemoteAddr()), sourceId, s.conn, s.stack)
	sink.SetUrlValues(values)

	log.Sugar.Infof("rtmp onplay app:%s stream:%s sink:%v conn:%s", app, stream_, sink.Id(), s.conn.RemoteAddr().String())

	_, state := stream.PreparePlaySink(sink)
	if utils.HookStateOK != state {
		log.Sugar.Errorf("rtmp拉流失败 source:%s sink:%s", sourceId, sink.Id())
	} else {
		s.handle = sink
	}

	response <- state
}

func (s *Session) Input(conn net.Conn, data []byte) error {
	//如果是推流，并且握手成功，后续收到的包，都将发送给LoopEvent处理
	if s.isPublisher {
		s.handle.(*Publisher).PublishSource.Input(data)
		return nil
	} else {
		return s.stack.Input(conn, data)
	}
}

func (s *Session) Close() {
	//session/conn/stack相关引用, go释放不了...手动赋值为nil
	s.conn = nil
	//释放协议栈
	if s.stack != nil {
		s.stack.Close()
		s.stack = nil
	}

	//还没到publish/play
	if s.handle == nil {
		return
	}

	publisher, ok := s.handle.(*Publisher)
	if ok {
		log.Sugar.Infof("rtmp推流结束 %s", publisher.PrintInfo())

		if s.isPublisher {
			s.handle.(*Publisher).Close()
			s.receiveBuffer = nil
		}
	} else {
		sink := s.handle.(*Sink)
		log.Sugar.Infof("rtmp拉流结束 %s", sink.PrintInfo())
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
