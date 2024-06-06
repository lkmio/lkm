package stream

import (
	"github.com/yangjiechina/avformat/utils"
)

type StreamManager struct {
	streams []utils.AVStream
}

func (s *StreamManager) Add(stream utils.AVStream) {
	for _, stream_ := range s.streams {
		utils.Assert(stream_.Type() != stream.Type())
		utils.Assert(stream_.CodecId() != stream.CodecId())
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
