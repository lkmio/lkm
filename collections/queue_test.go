package collections

import (
	"fmt"
	"testing"
)

func TestQueue(t *testing.T) {
	queue := NewQueue(1)

	for i := 0; i < 100; i++ {
		queue.Push(i)
	}

	for i := 0; i < 100; i++ {
		pop := queue.PopBack()
		println(fmt.Sprintf("element:%d", pop.(int)))
	}
}
