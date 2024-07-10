package stream

import (
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/collections"
)

// GOPBuffer GOP缓存
type GOPBuffer interface {

	// AddPacket Return bool 缓存帧是否成功, 如果首帧非关键帧, 缓存失败
	AddPacket(packet utils.AVPacket) bool

	// SetDiscardHandler 设置丢弃帧时的回调
	SetDiscardHandler(handler func(packet utils.AVPacket))

	PeekAll(handler func(packet utils.AVPacket))

	Peek(index int) utils.AVPacket

	Size() int

	Clear()

	Close()
}

type streamBuffer struct {
	buffer             collections.RingBuffer
	existVideoKeyFrame bool
	discardHandler     func(packet utils.AVPacket)
}

func NewStreamBuffer() GOPBuffer {
	return &streamBuffer{buffer: collections.NewRingBuffer(1000), existVideoKeyFrame: false}
}

func (s *streamBuffer) AddPacket(packet utils.AVPacket) bool {
	//缓存满,清空
	if s.Size()+1 == s.buffer.Capacity() {
		s.Clear()
	}

	//丢弃首帧视频非关键帧
	if utils.AVMediaTypeVideo == packet.MediaType() && !s.existVideoKeyFrame && !packet.KeyFrame() {
		return false
	}

	//丢弃前一组GOP
	videoKeyFrame := utils.AVMediaTypeVideo == packet.MediaType() && packet.KeyFrame()
	if videoKeyFrame {
		if s.existVideoKeyFrame {
			s.discard()
		}

		s.existVideoKeyFrame = true
	}

	s.buffer.Push(packet)
	return true
}

func (s *streamBuffer) SetDiscardHandler(handler func(packet utils.AVPacket)) {
	s.discardHandler = handler
}

func (s *streamBuffer) discard() {
	for s.buffer.Size() > 0 {
		pkt := s.buffer.Pop()

		if s.discardHandler != nil {
			s.discardHandler(pkt.(utils.AVPacket))
		}
	}

	s.existVideoKeyFrame = false
}

func (s *streamBuffer) Peek(index int) utils.AVPacket {
	utils.Assert(index < s.buffer.Size())
	head, tail := s.buffer.Data()

	if index < len(head) {
		return head[index].(utils.AVPacket)
	} else {
		return tail[index-len(head)].(utils.AVPacket)
	}
}

func (s *streamBuffer) PeekAll(handler func(packet utils.AVPacket)) {
	head, tail := s.buffer.Data()

	if head == nil {
		return
	}
	for _, value := range head {
		handler(value.(utils.AVPacket))
	}

	if tail == nil {
		return
	}
	for _, value := range tail {
		handler(value.(utils.AVPacket))
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
