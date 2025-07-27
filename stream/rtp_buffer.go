package stream

import (
	"github.com/lkmio/avformat/collections"
)

type RtpBuffer struct {
	queue *collections.Queue[*collections.ReferenceCounter[[]byte]]
}

func (r *RtpBuffer) Get() *collections.ReferenceCounter[[]byte] {
	if r.queue.Size() > 0 {
		pkt := r.queue.Peek(0)
		if pkt.UseCount() < 2 {
			r.queue.Pop()
			r.Put(pkt)
			return pkt
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

// Put 归还rtp包
func (r *RtpBuffer) Put(pkt *collections.ReferenceCounter[[]byte]) {
	bytes := pkt.Get()
	pkt.ResetData(bytes[:cap(bytes)])
	r.queue.Push(pkt)
}

func NewRtpBuffer(capacity int) *RtpBuffer {
	return &RtpBuffer{queue: collections.NewQueue[*collections.ReferenceCounter[[]byte]](capacity)}
}
