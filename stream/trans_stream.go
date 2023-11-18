package stream

import (
	"github.com/yangjiechina/avformat"
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
	avformat.Assert(len_ > 0 && len_ < 8)

	var streamId uint64
	streamId = uint64(protocol) << 56

	for i, id := range ids {
		bId, ok := narrowCodecIds[int(id)]
		avformat.Assert(ok)

		streamId |= uint64(bId) << (48 - i*8)
	}

	return TransStreamId(streamId)
}*/

func GenerateTransStreamId(protocol Protocol, ids ...utils.AVStream) TransStreamId {
	len_ := len(ids)
	avformat.Assert(len_ > 0 && len_ < 8)

	var streamId uint64
	streamId = uint64(protocol) << 56

	for i, id := range ids {
		bId, ok := narrowCodecIds[int(id.CodecId())]
		avformat.Assert(ok)

		streamId |= uint64(bId) << (48 - i*8)
	}

	return TransStreamId(streamId)
}

var TransStreamFactory func(protocol Protocol, streams []utils.AVStream) ITransStream

type ITransStream interface {
	AddTrack(stream utils.AVStream)

	WriteHeader()

	AddSink(sink ISink)

	RemoveSink(id SinkId)

	AllSink() []ISink
}

type TransStreamImpl struct {
	sinks  map[SinkId]ISink
	muxer  avformat.Muxer
	tracks []utils.AVStream
}

func (t *TransStreamImpl) AddTrack(stream utils.AVStream) {
	t.tracks = append(t.tracks, stream)
}

func (t *TransStreamImpl) AddSink(sink ISink) {
	t.sinks[sink.Id()] = sink
}

func (t *TransStreamImpl) RemoveSink(id SinkId) {
	delete(t.sinks, id)
}

func (t *TransStreamImpl) AllSink() []ISink {
	//TODO implement me
	panic("implement me")
}
