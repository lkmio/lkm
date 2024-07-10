package stream

import (
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/log"
)

// TransStream 将AVPacket封装成传输流，转发给各个Sink
type TransStream interface {
	Init()

	Input(packet utils.AVPacket) error

	AddTrack(stream utils.AVStream) error

	WriteHeader() error

	AddSink(sink Sink) error

	ExistSink(id SinkId) bool

	RemoveSink(id SinkId) (Sink, bool)

	PopAllSink(handler func(sink Sink))

	AllSink() []Sink

	Close() error

	SendPacket(data []byte) error
}

type BaseTransStream struct {
	Sinks map[SinkId]Sink
	//muxer      stream.Muxer
	Tracks     []utils.AVStream
	Completed  bool
	ExistVideo bool
}

func (t *BaseTransStream) Init() {
	t.Sinks = make(map[SinkId]Sink, 64)
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
	t.Sinks[sink.Id()] = sink
	sink.Start()
	return nil
}

func (t *BaseTransStream) ExistSink(id SinkId) bool {
	_, ok := t.Sinks[id]
	return ok
}

func (t *BaseTransStream) RemoveSink(id SinkId) (Sink, bool) {
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

type TCPTransStream struct {
	BaseTransStream
}

func (t *TCPTransStream) AddSink(sink Sink) error {
	if err := t.BaseTransStream.AddSink(sink); err != nil {
		return err
	}

	sink.GetConn().(*transport.Conn).EnableAsyncWriteMode(AppConfig.WriteBufferNumber - 1)
	return nil
}

func (t *TCPTransStream) SendPacket(data []byte) error {
	for _, sink := range t.Sinks {
		err := sink.Input(data)
		if err == nil {
			continue
		}

		if _, ok := err.(*transport.ZeroWindowSizeError); ok {
			log.Sugar.Errorf("发送超时, 强制删除 sink:%s", sink.PrintInfo())
			go sink.Close()
		}
	}

	return nil
}
