package stream

import (
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
)

// GOPBuffer GOP缓存
type GOPBuffer interface {

	// AddPacket Return bool 缓存帧是否成功, 如果首帧非关键帧, 缓存失败
	AddPacket(packet *avformat.AVPacket) bool

	PeekAll(handler func(packet *avformat.AVPacket))

	Peek(index int) *avformat.AVPacket

	PopAll(handler func(packet *avformat.AVPacket))

	RequiresClear(nextPacket *avformat.AVPacket) bool

	Size() int
}

type streamBuffer struct {
	buffer           collections.RingBuffer[*avformat.AVPacket]
	hasVideoKeyFrame bool
}

func (s *streamBuffer) AddPacket(packet *avformat.AVPacket) bool {
	if utils.AVMediaTypeVideo == packet.MediaType {
		if packet.Key {
			s.hasVideoKeyFrame = true
		} else if !s.hasVideoKeyFrame {
			// 丢弃首帧视频非关键帧
			return false
		}
	}

	s.buffer.Push(packet)
	return true
}

func (s *streamBuffer) Peek(index int) *avformat.AVPacket {
	utils.Assert(index < s.buffer.Size())
	head, tail := s.buffer.Data()

	if index < len(head) {
		return head[index]
	} else {
		return tail[index-len(head)]
	}
}

func (s *streamBuffer) PeekAll(handler func(packet *avformat.AVPacket)) {
	head, tail := s.buffer.Data()

	if head != nil {
		for _, value := range head {
			handler(value)
		}
	}

	if tail != nil {
		for _, value := range tail {
			handler(value)
		}
	}
}

func (s *streamBuffer) Size() int {
	return s.buffer.Size()
}

func (s *streamBuffer) PopAll(handler func(packet *avformat.AVPacket)) {
	for s.buffer.Size() > 0 {
		pkt := s.buffer.Pop()
		handler(pkt)
	}

	s.hasVideoKeyFrame = false
}

func (s *streamBuffer) RequiresClear(nextPacket *avformat.AVPacket) bool {
	return s.Size()+1 == s.buffer.Capacity() || (s.hasVideoKeyFrame && utils.AVMediaTypeVideo == nextPacket.MediaType && nextPacket.Key)
}

func NewStreamBuffer() GOPBuffer {
	return &streamBuffer{buffer: collections.NewRingBuffer[*avformat.AVPacket](1000), hasVideoKeyFrame: false}
}
