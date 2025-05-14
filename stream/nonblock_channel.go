package stream

import "github.com/lkmio/avformat/collections"

type NonBlockingChannel[T any] struct {
	Channel      chan T
	PendingQueue *collections.LinkedList[T]
	zero         T
}

func (p *NonBlockingChannel[T]) Post(event T) {
	for oldSize := 0; p.PendingQueue.Size() > 0 && p.PendingQueue.Size() != oldSize; {
		oldSize = p.PendingQueue.Size()
		select {
		case p.Channel <- p.PendingQueue.Get(0):
			p.PendingQueue.Remove(0)
		default:
			break
		}
	}

	if p.PendingQueue.Size() != 0 {
		p.PendingQueue.Add(event)
	} else {
		select {
		case p.Channel <- event:
			break
		default:
			p.PendingQueue.Add(event)
		}
	}
}

func (p *NonBlockingChannel[T]) Pop() T {
	if len(p.Channel) > 0 {
		select {
		case event := <-p.Channel:
			return event
		}
	}

	if p.PendingQueue.Size() > 0 {
		return p.PendingQueue.Remove(0)
	}

	return p.zero
}

func NewNonBlockingChannel[T any](size int) *NonBlockingChannel[T] {
	return &NonBlockingChannel[T]{
		Channel:      make(chan T, size),
		PendingQueue: &collections.LinkedList[T]{},
	}
}
