package stream

import (
	"github.com/lkmio/avformat/collections"
)

type RtpBuffer struct {
	queue *collections.Queue[*collections.ReferenceCounter[[]byte]]
}

func (r *RtpBuffer) Get() *collections.ReferenceCounter[[]byte] {
	if r.queue.Size() > 0 {
		rtp := r.queue.Peek(0)
		if rtp.UseCount() < 2 {
			bytes := rtp.Get()
			rtp.ResetData(bytes[:cap(bytes)])
			return rtp
		}
	}

	bytes := collections.NewReferenceCounter(UDPReceiveBufferPool.Get().([]byte))
	r.queue.Push(bytes)
	return bytes
}

func (r *RtpBuffer) Clear() {
	for r.queue.Size() > 0 {
		if r.queue.Peek(0).UseCount() > 1 {
			break
		}

		bytes := r.queue.Pop().Get()
		UDPReceiveBufferPool.Put(bytes[:cap(bytes)])
	}
}

func NewRtpBuffer(capacity int) *RtpBuffer {
	return &RtpBuffer{queue: collections.NewQueue[*collections.ReferenceCounter[[]byte]](capacity)}
}
