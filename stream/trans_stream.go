package stream

import (
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
)

// TransStream 将AVPacket封装成传输流，转发给各个Sink
type TransStream interface {
	Init()

	Input(packet utils.AVPacket) error

	AddTrack(stream utils.AVStream) error

	WriteHeader() error

	AddSink(sink Sink) error

	ExistSink(id SinkID) bool

	RemoveSink(id SinkID) (Sink, bool)

	PopAllSink(handler func(sink Sink))

	AllSink() []Sink

	Close() error

	SendPacket(data []byte) error

	Protocol() TransStreamProtocol
}

type BaseTransStream struct {
	Sinks map[SinkID]Sink
	//muxer      stream.Muxer
	Tracks     []utils.AVStream
	Completed  bool
	ExistVideo bool
	Protocol_  TransStreamProtocol
}

func (t *BaseTransStream) Init() {
	t.Sinks = make(map[SinkID]Sink, 64)
}

func (t *BaseTransStream) Input(packet utils.AVPacket) error {
	return nil
}

func (t *BaseTransStream) AddTrack(stream utils.AVStream) error {
	t.Tracks = append(t.Tracks, stream)
	if utils.AVMediaTypeVideo == stream.Type() {
		t.ExistVideo = true
	}
	return nil
}

func (t *BaseTransStream) AddSink(sink Sink) error {
	t.Sinks[sink.GetID()] = sink
	sink.Start()
	return nil
}

func (t *BaseTransStream) ExistSink(id SinkID) bool {
	_, ok := t.Sinks[id]
	return ok
}

func (t *BaseTransStream) RemoveSink(id SinkID) (Sink, bool) {
	sink, ok := t.Sinks[id]
	if ok {
		delete(t.Sinks, id)
	}

	return sink, ok
}

func (t *BaseTransStream) PopAllSink(handler func(sink Sink)) {
	for _, sink := range t.Sinks {
		handler(sink)
	}

	t.Sinks = nil
}

func (t *BaseTransStream) AllSink() []Sink {
	//TODO implement me
	panic("implement me")
}

func (t *BaseTransStream) Close() error {
	return nil
}

func (t *BaseTransStream) SendPacket(data []byte) error {
	for _, sink := range t.Sinks {
		sink.Input(data)
	}

	return nil
}

func (t *BaseTransStream) Protocol() TransStreamProtocol {
	return t.Protocol_
}

type TCPTransStream struct {
	BaseTransStream
}

func (t *TCPTransStream) AddSink(sink Sink) error {
	if err := t.BaseTransStream.AddSink(sink); err != nil {
		return err
	}

	if sink.GetConn() != nil {
		sink.GetConn().(*transport.Conn).EnableAsyncWriteMode(AppConfig.WriteBufferNumber - 1)
	}
	return nil
}

func (t *TCPTransStream) SendPacket(data []byte) error {
	for _, sink := range t.Sinks {
		err := sink.Input(data)
		if err == nil {
			continue
		}

		if _, ok := err.(*transport.ZeroWindowSizeError); ok {
			log.Sugar.Errorf("发送超时, 强制断开连接 sink:%s", sink.String())
			sink.GetConn().Close()
		}
	}

	return nil
}
