package stream

type RingBuffer interface {
	IsEmpty() bool

	IsFull() bool

	Push(value interface{})

	Pop() interface{}

	Head() interface{}

	Tail() interface{}

	Size() int
}

func NewRingBuffer(capacity int) RingBuffer {
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
	r.head = (r.head + 1) % cap(r.data)
	r.size--
	return element
}

func (r *ringBuffer) Head() interface{} {
	return r.data[r.head]
}

func (r *ringBuffer) Tail() interface{} {
	return r.data[r.tail]
}

func (r *ringBuffer) Size() int {
	return r.size
}
