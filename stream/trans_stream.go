package stream

import (
	"github.com/yangjiechina/avformat/stream"
	"github.com/yangjiechina/avformat/utils"
)

// TransStreamId 每个传输流的唯一Id，由协议+流Id组成
type TransStreamId uint64

// AVCodecID转为byte的对应关系
var narrowCodecIds map[int]byte

func init() {
	narrowCodecIds = map[int]byte{
		int(utils.AVCodecIdH263): 0x1,
		int(utils.AVCodecIdH264): 0x2,
		int(utils.AVCodecIdH265): 0x3,
		int(utils.AVCodecIdAV1):  0x4,
		int(utils.AVCodecIdVP8):  0x5,
		int(utils.AVCodecIdVP9):  0x6,

		int(utils.AVCodecIdAAC):  101,
		int(utils.AVCodecIdMP3):  102,
		int(utils.AVCodecIdOPUS): 103,
	}
}

// GenerateTransStreamId 根据传入的推拉流协议和编码器ID生成StreamId
// 请确保ids根据值升序排序传参
/*func GenerateTransStreamId(protocol Protocol, ids ...utils.AVCodecID) TransStreamId {
	len_ := len(ids)
	utils.Assert(len_ > 0 && len_ < 8)

	var streamId uint64
	streamId = uint64(protocol) << 56

	for i, id := range ids {
		bId, ok := narrowCodecIds[int(id)]
		utils.Assert(ok)

		streamId |= uint64(bId) << (48 - i*8)
	}

	return TransStreamId(streamId)
}*/

func GenerateTransStreamId(protocol Protocol, ids ...utils.AVStream) TransStreamId {
	len_ := len(ids)
	utils.Assert(len_ > 0 && len_ < 8)

	var streamId uint64
	streamId = uint64(protocol) << 56

	for i, id := range ids {
		bId, ok := narrowCodecIds[int(id.CodecId())]
		utils.Assert(ok)

		streamId |= uint64(bId) << (48 - i*8)
	}

	return TransStreamId(streamId)
}

var TransStreamFactory func(source ISource, protocol Protocol, streams []utils.AVStream) ITransStream

// ITransStream 讲AVPacket封装成传输流，转发给各个Sink
type ITransStream interface {
	Input(packet utils.AVPacket) error

	AddTrack(stream utils.AVStream) error

	WriteHeader() error

	AddSink(sink ISink) error

	RemoveSink(id SinkId) (ISink, bool)

	PopAllSink(handler func(sink ISink))

	AllSink() []ISink

	Close() error
}

type TransStreamImpl struct {
	Sinks       map[SinkId]ISink
	muxer       stream.Muxer
	Tracks      []utils.AVStream
	transBuffer MemoryPool //每个TransStream也缓存封装后的流
	Completed   bool
	existVideo  bool
}

func (t *TransStreamImpl) Input(packet utils.AVPacket) error {
	return nil
}

func (t *TransStreamImpl) AddTrack(stream utils.AVStream) error {
	t.Tracks = append(t.Tracks, stream)
	if utils.AVMediaTypeVideo == stream.Type() {
		t.existVideo = true
	}
	return nil
}

func (t *TransStreamImpl) AddSink(sink ISink) error {
	t.Sinks[sink.Id()] = sink
	return nil
}

func (t *TransStreamImpl) RemoveSink(id SinkId) (ISink, bool) {
	sink, ok := t.Sinks[id]
	if ok {
		delete(t.Sinks, id)
	}

	return sink, ok
}

func (t *TransStreamImpl) PopAllSink(handler func(sink ISink)) {
	for _, sink := range t.Sinks {
		handler(sink)
	}

	t.Sinks = nil
}

func (t *TransStreamImpl) AllSink() []ISink {
	//TODO implement me
	panic("implement me")
}

func (t *TransStreamImpl) Close() error {
	return nil
}
