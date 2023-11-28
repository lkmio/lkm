package stream

import (
	"github.com/yangjiechina/avformat/utils"
)

type RingBuffer interface {
	IsEmpty() bool

	IsFull() bool

	Push(value interface{})

	Pop() interface{}

	Head() interface{}

	Tail() interface{}

	Size() int

	All() ([]interface{}, []interface{})
}

func NewRingBuffer(capacity int) RingBuffer {
	utils.Assert(capacity > 0)
	r := &ringBuffer{
		data: make([]interface{}, capacity),
		head: 0,
		tail: 0,
		size: 0,
	}

	return r
}

type ringBuffer struct {
	data []interface{}
	head int
	tail int
	size int
}

func (r *ringBuffer) IsEmpty() bool {
	return r.size == 0
}

func (r *ringBuffer) IsFull() bool {
	return r.size == cap(r.data)
}

func (r *ringBuffer) Push(value interface{}) {
	if r.IsFull() {
		r.Pop()
	}

	r.data[r.tail] = value
	r.tail = (r.tail + 1) % cap(r.data)

	r.size++
}

func (r *ringBuffer) Pop() interface{} {
	if r.IsEmpty() {
		return nil
	}

	element := r.data[r.head]
	r.data[r.head] = nil
	r.head = (r.head + 1) % cap(r.data)
	r.size--
	return element
}

func (r *ringBuffer) Head() interface{} {
	return r.data[r.head]
}

func (r *ringBuffer) Tail() interface{} {
	return r.data[utils.MaxInt(0, r.tail-1)]
}

func (r *ringBuffer) Size() int {
	return r.size
}

func (r *ringBuffer) All() ([]interface{}, []interface{}) {
	if r.size == 0 {
		return nil, nil
	}

	if r.head <= r.tail {
		return r.data[r.head:], r.data[:r.tail]
	} else {
		return r.data[r.head:], nil
	}
}
