package stream

import "math"

// JitterBuffer 只处理乱序的JitterBuffer
type JitterBuffer[T comparable] struct {
	maxSeqNum  uint16
	minSeqNum  uint16
	nextSeqNum uint16

	count         int
	minStartCount int

	first bool
	queue []T
	zero  T
}

func (j *JitterBuffer[T]) emit() T {
	if j.first {
		j.nextSeqNum = j.minSeqNum
		j.first = false
	}
	if j.nextSeqNum > j.maxSeqNum {
		j.nextSeqNum = j.minSeqNum
	}

	for j.queue[j.nextSeqNum] == j.zero {
		j.nextSeqNum++
	}

	t := j.queue[j.nextSeqNum]
	j.queue[j.nextSeqNum] = j.zero
	j.nextSeqNum++
	j.minSeqNum = uint16(math.Min(float64(j.nextSeqNum), float64(j.maxSeqNum)))
	j.count--
	return t
}

func (j *JitterBuffer[T]) Push(seq uint16, packet T) {
	if j.count == 0 {
		j.minSeqNum = seq
		j.maxSeqNum = seq
	}

	if j.queue[seq] == j.zero {
		j.queue[seq] = packet
		j.count++
	}

	j.minSeqNum = uint16(math.Min(float64(j.minSeqNum), float64(seq)))
	j.maxSeqNum = uint16(math.Max(float64(j.maxSeqNum), float64(seq)))
}

func (j *JitterBuffer[T]) Pop(sort bool) T {
	if sort {
		if j.count > j.minStartCount {
			return j.emit()
		}
	} else {
		for j.count > 0 {
			return j.emit()
		}
	}

	return j.zero
}

func NewJitterBuffer[T comparable]() *JitterBuffer[T] {
	return &JitterBuffer[T]{
		queue:         make([]T, 0xFFFF+1),
		minStartCount: 50,
		first:         true,
	}
}
