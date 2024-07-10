package collections

import (
	"github.com/lkmio/avformat/libbufio"
	"github.com/lkmio/avformat/utils"
)

type Queue struct {
	*ringBuffer
}

func NewQueue(capacity int) *Queue {
	utils.Assert(capacity > 0)

	return &Queue{ringBuffer: &ringBuffer{
		data: make([]interface{}, capacity),
		head: 0,
		tail: 0,
		size: 0,
	}}
}

func (q *Queue) Push(value interface{}) {
	if q.ringBuffer.IsFull() {
		newArray := make([]interface{}, q.ringBuffer.Size()*2)
		head, tail := q.ringBuffer.Data()
		copy(newArray, head)
		if tail != nil {
			copy(newArray[len(head):], tail)
		}

		q.data = newArray
		q.head = 0
		q.tail = q.size
	}

	q.data[q.tail] = value
	q.tail = (q.tail + 1) % cap(q.data)

	q.size++
}

func (q *Queue) PopBack() interface{} {
	utils.Assert(q.size > 0)

	value := q.ringBuffer.Tail()
	q.size--
	q.tail = libbufio.MaxInt(0, q.tail-1)

	return value
}
