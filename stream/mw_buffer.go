package stream

// MergeWritingBuffer 实现针对RTMP/FLV/HLS等基于TCP传输流的合并写缓存
// 和GOP缓存一样, 也以视频关键帧为界. 遇到视频关键帧, 发送剩余输出流, 清空buffer

type MergeWritingBuffer interface {
	Allocate(size int) []byte

	// PeekCompletedSegment 返回当前完整合并写切片
	PeekCompletedSegment(ts int64) []byte

	// PopSegment 返回当前合并写切片, 并清空内存池
	PopSegment() []byte

	// SegmentList 返回所有完整切片
	SegmentList() []byte

	IsFull(ts int64) bool

	IsCompeted() bool

	IsEmpty() bool

	Reserve(count int)
}

type mergeWritingBuffer struct {
	transStreamBuffer MemoryPool

	segmentOffset int //当前合并写包位于memoryPool的开始偏移量

	prePacketTS int64 //前一个包的时间戳
}

func (m *mergeWritingBuffer) Allocate(size int) []byte {
	return m.transStreamBuffer.Allocate(size)
}

func (m *mergeWritingBuffer) PeekCompletedSegment(ts int64) []byte {
	if !AppConfig.GOPCache {
		data, _ := m.transStreamBuffer.Data()
		m.transStreamBuffer.Clear()
		return data
	}

	if m.prePacketTS == -1 {
		m.prePacketTS = ts
	}

	if ts < m.prePacketTS {
		m.prePacketTS = ts
	}

	if int(ts-m.prePacketTS) < AppConfig.MergeWriteLatency {
		return nil
	}

	head, _ := m.transStreamBuffer.Data()
	data := head[m.segmentOffset:]

	m.segmentOffset = len(head)
	m.prePacketTS = -1

	return data
}

func (m *mergeWritingBuffer) IsFull(ts int64) bool {
	if m.prePacketTS == -1 {
		return false
	}

	return int(ts-m.prePacketTS) >= AppConfig.MergeWriteLatency
}

func (m *mergeWritingBuffer) IsCompeted() bool {
	data, _ := m.transStreamBuffer.Data()
	return m.segmentOffset == len(data)
}

func (m *mergeWritingBuffer) IsEmpty() bool {
	data, _ := m.transStreamBuffer.Data()
	return len(data) <= m.segmentOffset
}

func (m *mergeWritingBuffer) Reserve(count int) {
	_ = m.transStreamBuffer.Allocate(count)
}

func (m *mergeWritingBuffer) PopSegment() []byte {
	if !AppConfig.GOPCache {
		return nil
	}

	head, _ := m.transStreamBuffer.Data()
	data := head[m.segmentOffset:]
	m.transStreamBuffer.Clear()
	m.segmentOffset = 0
	m.prePacketTS = -1
	return data
}

func (m *mergeWritingBuffer) SegmentList() []byte {
	if !AppConfig.GOPCache {
		return nil
	}

	head, _ := m.transStreamBuffer.Data()
	return head[:m.segmentOffset]
}

func NewMergeWritingBuffer(existVideo bool) MergeWritingBuffer {
	//开启GOP缓存, 输出流也缓存整个GOP
	bufferSize := AppConfig.GOPBufferSize
	if existVideo && !AppConfig.GOPCache {
		bufferSize = 1024 * 1000
	} else if !existVideo {
		bufferSize = 48000 * 10
	}

	return &mergeWritingBuffer{
		transStreamBuffer: NewDirectMemoryPool(bufferSize),
		segmentOffset:     0,
		prePacketTS:       -1,
	}
}
