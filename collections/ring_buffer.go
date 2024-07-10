package collections

import (
	"github.com/lkmio/avformat/utils"
)

type RingBuffer interface {
	IsEmpty() bool

	IsFull() bool

	Push(value interface{})

	Pop() interface{}

	Head() interface{}

	Tail() interface{}

	Size() int

	Capacity() int

	Data() ([]interface{}, []interface{})

	Clear()
}

func NewRingBuffer(capacity int) RingBuffer {
	utils.Assert(capacity > 0)
	r := &ringBuffer{
		data:     make([]interface{}, capacity),
		head:     0,
		tail:     0,
		size:     0,
		capacity: capacity,
	}

	return r
}

type ringBuffer struct {
	data     []interface{}
	head     int
	tail     int
	size     int
	capacity int
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
	utils.Assert(!r.IsEmpty())
	return r.data[r.head]
}

func (r *ringBuffer) Tail() interface{} {
	utils.Assert(!r.IsEmpty())
	if r.tail > 0 {
		return r.data[r.tail-1]
	} else {
		return r.data[cap(r.data)-1]
	}
}

func (r *ringBuffer) Size() int {
	return r.size
}

func (r *ringBuffer) Capacity() int {
	return r.capacity
}

func (r *ringBuffer) Data() ([]interface{}, []interface{}) {
	if r.size == 0 {
		return nil, nil
	}

	if r.tail <= r.head {
		return r.data[r.head:], r.data[:r.tail]
	} else {
		return r.data[r.head:r.tail], nil
	}
}

func (r *ringBuffer) Clear() {
	r.size = 0
	r.head = 0
	r.tail = 0
}
