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

	// SetDiscardHandler 设置丢弃帧时的回调
	SetDiscardHandler(handler func(packet *avformat.AVPacket))

	PeekAll(handler func(packet *avformat.AVPacket))

	Peek(index int) *avformat.AVPacket

	Size() int

	Clear()

	Close()
}

type streamBuffer struct {
	buffer             collections.RingBuffer[*avformat.AVPacket]
	existVideoKeyFrame bool
	discardHandler     func(packet *avformat.AVPacket)
}

func (s *streamBuffer) AddPacket(packet *avformat.AVPacket) bool {
	// 缓存满,清空
	if s.Size()+1 == s.buffer.Capacity() {
		s.Clear()
	}

	// 丢弃首帧视频非关键帧
	if utils.AVMediaTypeVideo == packet.MediaType && !s.existVideoKeyFrame && !packet.Key {
		return false
	}

	// 丢弃前一组GOP
	videoKeyFrame := utils.AVMediaTypeVideo == packet.MediaType && packet.Key
	if videoKeyFrame {
		if s.existVideoKeyFrame {
			s.discard()
		}

		s.existVideoKeyFrame = true
	}

	s.buffer.Push(packet)
	return true
}

func (s *streamBuffer) SetDiscardHandler(handler func(packet *avformat.AVPacket)) {
	s.discardHandler = handler
}

func (s *streamBuffer) discard() {
	for s.buffer.Size() > 0 {
		pkt := s.buffer.Pop()

		if s.discardHandler != nil {
			s.discardHandler(pkt)
		}
	}

	s.existVideoKeyFrame = false
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

func (s *streamBuffer) Clear() {
	s.discard()
}

func (s *streamBuffer) Close() {
	s.discardHandler = nil
}

func NewStreamBuffer() GOPBuffer {
	return &streamBuffer{buffer: collections.NewRingBuffer[*avformat.AVPacket](1000), existVideoKeyFrame: false}
}
