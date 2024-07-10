package collections

import (
	"fmt"
	"testing"
)

func TestRingBuffer(t *testing.T) {
	buffer := NewRingBuffer(10)
	full := buffer.IsFull()
	empty := buffer.IsEmpty()
	head := buffer.Head()
	tail := buffer.Tail()
	pop := buffer.Pop()

	println(full)
	println(empty)
	println(head)
	println(tail)
	println(pop)
	for i := 0; i < 100; i++ {
		buffer.Push(i)
	}

	for !buffer.IsEmpty() {
		i := buffer.Pop()
		println(fmt.Sprintf("element:%d", i.(int)))
	}
}
