package stream

import "math"

// JitterBuffer 只处理乱序的JitterBuffer
type JitterBuffer struct {
	maxSeqNum  uint16
	minSeqNum  uint16
	nextSeqNum uint16

	count         int
	minStartCount int

	first    bool
	queue    []interface{}
	onPacket func(packet interface{})
}

func (j *JitterBuffer) emit() {
	if j.first {
		j.nextSeqNum = j.minSeqNum
		j.first = false
	}
	if j.nextSeqNum > j.maxSeqNum {
		j.nextSeqNum = j.minSeqNum
	}

	for j.queue[j.nextSeqNum] == nil {
		j.nextSeqNum++
	}

	j.onPacket(j.queue[j.nextSeqNum])
	j.queue[j.nextSeqNum] = nil
	j.nextSeqNum++
	j.minSeqNum = uint16(math.Min(float64(j.nextSeqNum), float64(j.maxSeqNum)))
	j.count--
}

func (j *JitterBuffer) Push(seq uint16, packet interface{}) {
	if j.count == 0 {
		j.minSeqNum = seq
		j.maxSeqNum = seq
	}

	if j.queue[seq] == nil {
		j.queue[seq] = packet
		j.count++
	}

	j.minSeqNum = uint16(math.Min(float64(j.minSeqNum), float64(seq)))
	j.maxSeqNum = uint16(math.Max(float64(j.maxSeqNum), float64(seq)))

	if j.count > j.minStartCount {
		j.emit()
	}
}

func (j *JitterBuffer) Flush() {
	for j.count > 0 {
		j.emit()
	}
}

func (j *JitterBuffer) SetHandler(handler func(packet interface{})) {
	j.onPacket = handler
}

func NewJitterBuffer() *JitterBuffer {
	return &JitterBuffer{
		queue:         make([]interface{}, 0xFFFF+1),
		minStartCount: 50,
		first:         true,
	}
}
