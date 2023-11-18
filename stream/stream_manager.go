package stream

import (
	"github.com/yangjiechina/avformat"
	"github.com/yangjiechina/avformat/utils"
)

type Track interface {
	Stream() utils.AVStream

	Cache() RingBuffer

	AddPacket(packet utils.AVPacket)
}

// 封装stream 增加GOP管理
type track struct {
	stream      utils.AVStream
	cache       RingBuffer
	duration    int
	keyFrameDts int64 //最近一个关键帧的Dts
}

func (t *track) Stream() utils.AVStream {
	return t.stream
}

func (t *track) Cache() RingBuffer {
	return t.cache
}

func (t *track) AddPacket(packet utils.AVPacket) {
	if t.cache.IsEmpty() && !packet.KeyFrame() {
		return
	}

	t.cache.Push(packet)
	if packet.KeyFrame() {
		t.keyFrameDts = packet.Dts()
	}

	//以最近的关键帧时间戳开始，丢弃缓存超过duration长度的帧
	//至少需要保障当前GOP完整
	//head := t.cache.Head().(utils.AVPacket)
	//for farthest := t.keyFrameDts - int64(t.duration); t.cache.Size() > 1 && t.cache.Head().(utils.AVPacket).Dts() < farthest; {
	//	t.cache.Pop()
	//}
}

func NewTrack(stream utils.AVStream, cacheSeconds int) Track {
	t := &track{stream: stream, duration: cacheSeconds * 1000}

	if cacheSeconds > 0 {
		if utils.AVMediaTypeVideo == stream.Type() {
			t.cache = NewRingBuffer(cacheSeconds * 30 * 2)
		} else if utils.AVMediaTypeAudio == stream.Type() {
			t.cache = NewRingBuffer(cacheSeconds * 50 * 2)
		}
	}

	return t
}

type StreamManager struct {
	streams   []utils.AVStream
	completed bool
}

func (s *StreamManager) Add(stream utils.AVStream) {
	for _, stream_ := range s.streams {
		avformat.Assert(stream_.Type() != stream.Type())
		avformat.Assert(stream_.CodecId() != stream.CodecId())
	}

	s.streams = append(s.streams, stream)

	//按照AVCodecId升序排序
	for i := 0; i < len(s.streams); i++ {
		for j := 1; j < len(s.streams)-i; j++ {
			tmp := s.streams[j-1]
			if s.streams[j].CodecId() < tmp.CodecId() {
				s.streams[j-1] = s.streams[j]
				s.streams[j] = tmp
			}
		}
	}
}

func (s *StreamManager) FindStream(id utils.AVCodecID) utils.AVStream {
	for _, stream_ := range s.streams {
		if stream_.CodecId() == id {
			return stream_
		}
	}

	return nil
}

func (s *StreamManager) FindStreamWithType(mediaType utils.AVMediaType) utils.AVStream {
	for _, stream_ := range s.streams {
		if stream_.Type() == mediaType {
			return stream_
		}
	}

	return nil
}

func (s *StreamManager) FindStreams(id utils.AVCodecID) []utils.AVStream {
	var streams []utils.AVStream
	for _, stream_ := range s.streams {
		if stream_.CodecId() == id {
			streams = append(streams, stream_)
		}
	}

	return streams
}

func (s *StreamManager) FindStreamsWithType(mediaType utils.AVMediaType) []utils.AVStream {
	var streams []utils.AVStream
	for _, stream_ := range s.streams {
		if stream_.Type() == mediaType {
			streams = append(streams, stream_)
		}
	}

	return streams
}

func (s *StreamManager) All() []utils.AVStream {
	return s.streams
}
