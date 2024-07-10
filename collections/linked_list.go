package collections

import "github.com/yangjiechina/avformat/utils"

type Node[T any] struct {
	data T
	next *Node[T]
}

type LinkedList[T any] struct {
	fist *Node[T]
	last *Node[T]
	size int
}

func (l *LinkedList[T]) Add(data T) {
	node := &Node[T]{data: data, next: nil}
	if l.last != nil {
		l.last.next = node
		l.last = node
	} else {
		l.fist = node
		l.last = node
	}

	l.size++
}

func (l *LinkedList[T]) Remove(index int) T {
	utils.Assert(index < l.size)

	prevNode := l.fist
	offsetNode := l.fist
	for i := 0; i < l.size; i++ {
		if i == index {
			break
		}

		prevNode = offsetNode
		offsetNode = offsetNode.next
	}

	if offsetNode == l.fist {
		//删除第一个node
		l.fist = l.fist.next
	} else if offsetNode == l.last {
		//删除最后一个node
		l.last = prevNode
	} else {
		prevNode.next = offsetNode.next
	}

	if l.size--; l.size == 0 {
		l.fist = nil
		l.last = nil
	}
	return offsetNode.data
}

func (l *LinkedList[T]) Get(index int) T {
	utils.Assert(index < l.size)

	offsetNode := l.fist
	for i := 0; i < l.size; i++ {
		if i == index {
			break
		}

		offsetNode = offsetNode.next
	}

	return offsetNode.data
}

func (l *LinkedList[T]) Size() int {

	return l.size
}
