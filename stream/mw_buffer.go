package stream

import (
	"github.com/lkmio/avformat/bufio"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/avformat/utils"
)

const (
	BlockBufferSize  = 1024 * 1024 * 2
	BlockBufferCount = 4
)

// MergeWritingBuffer 实现针对RTMP/FLV/HLS等基于TCP传输流的合并写缓存
type MergeWritingBuffer interface {
	TryGrow() bool

	TryAlloc(size int, ts int64, videoPkt, videoKey bool) ([]byte, bool)

	// TryFlushSegment 尝试生成切片, 如果时长不足, 返回nil
	TryFlushSegment() ([]byte, bool)

	// FlushSegment 生成并返回当前切片, 以及是否是关键帧切片.
	FlushSegment() ([]byte, bool)

	// ShouldFlush 当前切片是否已达到生成条件
	ShouldFlush(ts int64) bool

	// IsNewSegment 当前切片是否还未写数据
	IsNewSegment() bool

	// Reserve 从当前切片中预留指定长度数据
	Reserve(length int)

	// ReadSegmentsFromKeyFrameIndex 返回最近的关键帧切片
	ReadSegmentsFromKeyFrameIndex(cb func([]byte))

	Capacity() int

	HasVideoDataInCurrentSegment() bool
}

type mergeWritingBuffer struct {
	buffers []struct {
		buffer              collections.BlockBuffer
		nextSegmentDataSize int
		preSegmentsDataSize int
		preSegmentCount     int

		prevSegments *collections.Queue[struct {
			data []byte
			key  bool
		}]
		segments *collections.Queue[struct {
			data []byte
			key  bool
		}]
	}

	index    int   // 当前使用内存池的索引
	startTS  int64 // 当前切片的开始时间
	duration int   // 当前切片时长

	hasKeyVideoDataInCurrentSegment   bool    // 当前切片是否存在关键视频帧
	hasVideoDataInCurrentSegment      bool    // 当前切片是否存在视频帧
	completedKeyVideoSegmentPositions []int64 // 完整视频关键帧切片的数量
	existVideo                        bool    // 是否存在视频
	segmentCount                      int     // 切片数量
}

func (m *mergeWritingBuffer) createBuffer(minSize int) collections.BlockBuffer {
	var size int
	if !m.existVideo {
		size = 1024 * 500
	} else {
		size = BlockBufferSize
	}

	return collections.NewDirectBlockBuffer(bufio.MaxInt(size, minSize))
}

func (m *mergeWritingBuffer) grow(minSize int) {
	m.buffers = append(m.buffers, struct {
		buffer              collections.BlockBuffer
		nextSegmentDataSize int
		preSegmentsDataSize int
		preSegmentCount     int
		prevSegments        *collections.Queue[struct {
			data []byte
			key  bool
		}]
		segments *collections.Queue[struct {
			data []byte
			key  bool
		}]
	}{buffer: m.createBuffer(minSize), prevSegments: collections.NewQueue[struct {
		data []byte
		key  bool
	}](64), segments: collections.NewQueue[struct {
		data []byte
		key  bool
	}](64)})
}

func (m *mergeWritingBuffer) TryGrow() bool {
	var ok bool
	if !m.existVideo {
		ok = len(m.buffers) < 1
	} else {
		ok = len(m.buffers) < BlockBufferCount
	}

	if ok {
		m.grow(0)
	}

	return ok
}

func (m *mergeWritingBuffer) RemoveSegment() {
	segment := m.buffers[m.index].prevSegments.Pop()
	m.buffers[m.index].nextSegmentDataSize += len(segment.data)
	m.segmentCount--

	if segment.key {
		m.completedKeyVideoSegmentPositions = m.completedKeyVideoSegmentPositions[1:]
	}
}

func (m *mergeWritingBuffer) TryAlloc(size int, ts int64, videoPkt, videoKey bool) ([]byte, bool) {
	length := len(m.buffers)
	if length < 1 {
		m.grow(size)
	}

	bytes := m.buffers[m.index].buffer.AvailableBytes()
	if bytes < size {
		// 非完整切片，先保存切片再分配新的内存
		if m.buffers[m.index].buffer.PendingBlockSize() > 0 {
			return nil, false
		}

		// 还未遇到2组GOP, 不能释放旧的内存池, 创建新的内存池
		// 其他情况, 调用tryAlloc, 手动申请内存
		if m.existVideo && AppConfig.GOPCache && len(m.completedKeyVideoSegmentPositions) < 2 {
			m.grow(size)
		}

		// 即将使用下一个内存池, 清空上次创建的切片
		for m.buffers[m.index].prevSegments.Size() > 0 {
			m.RemoveSegment()
		}

		// 使用下一块内存, 或者从头覆盖
		if m.index+1 < len(m.buffers) {
			m.index++
		} else {
			m.index = 0
		}

		// 复用内存池, 将未清空完的上上次创建的切片放在尾部
		//for m.buffers[m.index].prevSegments.Size() > 0 {
		//	m.buffers[m.index].segments.Push(m.buffers[m.index].prevSegments.Pop())
		//}

		// 复用内存池, 清空上上次创建的切片
		//for m.buffers[m.index].prevSegments.Size() > 0 {
		//	m.RemoveSegment()
		//}

		// 复用内存池, 保留上次内存池创建的切片
		m.buffers[m.index].nextSegmentDataSize = 0
		m.buffers[m.index].preSegmentsDataSize = 0
		m.buffers[m.index].preSegmentCount = m.buffers[m.index].segments.Size()
		m.buffers[m.index].buffer.Clear()
		if m.buffers[m.index].preSegmentCount > 0 {
			m.buffers[m.index].prevSegments.Clear()
			tmp := m.buffers[m.index].prevSegments
			m.buffers[m.index].prevSegments = m.buffers[m.index].segments
			m.buffers[m.index].segments = tmp
			m.RemoveSegment()
		}
	}

	// 复用旧的内存池, 减少计数
	if !m.buffers[m.index].prevSegments.IsEmpty() {
		totalSize := len(m.buffers[m.index].buffer.(*collections.DirectBlockBuffer).Data()) + size
		for !m.buffers[m.index].prevSegments.IsEmpty() && totalSize > m.buffers[m.index].nextSegmentDataSize {
			m.RemoveSegment()
		}
	}

	return m.alloc(size, ts, videoPkt, videoKey), true
}

func (m *mergeWritingBuffer) alloc(size int, ts int64, videoPkt, videoKey bool) []byte {
	utils.Assert(ts != -1)
	bytes := m.buffers[m.index].buffer.AvailableBytes()
	// 当前切片必须有足够空间, 否则先调用TryAlloc
	utils.Assert(bytes >= size)

	// 新的切片
	if m.startTS == -1 {
		m.startTS = ts
	}

	if !m.hasVideoDataInCurrentSegment && videoPkt {
		m.hasVideoDataInCurrentSegment = true
	}

	if videoKey {
		m.hasKeyVideoDataInCurrentSegment = true
	}

	if ts < m.startTS {
		m.startTS = ts
	}

	m.duration = int(ts - m.startTS)
	return m.buffers[m.index].buffer.Alloc(size)
}

func (m *mergeWritingBuffer) FlushSegment() ([]byte, bool) {
	data := m.buffers[m.index].buffer.Feat()
	if len(data) == 0 {
		return nil, false
	}

	m.segmentCount++
	key := m.hasKeyVideoDataInCurrentSegment
	m.hasKeyVideoDataInCurrentSegment = false
	if key {
		m.completedKeyVideoSegmentPositions = append(m.completedKeyVideoSegmentPositions, int64(m.index<<32|m.buffers[m.index].segments.Size()))
	}

	m.buffers[m.index].segments.Push(struct {
		data []byte
		key  bool
	}{data: data, key: key})

	// 清空下一个切片的标记
	m.startTS = -1
	m.duration = 0
	m.hasVideoDataInCurrentSegment = false
	return data, key
}

func (m *mergeWritingBuffer) TryFlushSegment() ([]byte, bool) {
	if (!AppConfig.GOPCache || !m.existVideo) || m.duration >= AppConfig.MergeWriteLatency {
		return m.FlushSegment()
	}

	return nil, false
}

func (m *mergeWritingBuffer) ShouldFlush(ts int64) bool {
	if m.startTS == -1 {
		return false
	}

	return int(ts-m.startTS) >= AppConfig.MergeWriteLatency
}

func (m *mergeWritingBuffer) IsNewSegment() bool {
	return m.buffers == nil || m.buffers[m.index].buffer.PendingBlockSize() == 0
}

func (m *mergeWritingBuffer) Reserve(size int) {
	_ = m.buffers[m.index].buffer.Alloc(size)
}

func (m *mergeWritingBuffer) ReadSegmentsFromKeyFrameIndex(cb func([]byte)) {
	if !AppConfig.GOPCache || !m.existVideo || len(m.completedKeyVideoSegmentPositions) < 1 {
		return
	}

	marker := m.completedKeyVideoSegmentPositions[len(m.completedKeyVideoSegmentPositions)-1]
	bufferIndex := int(marker >> 32 & 0xFFFFFFFF)
	position := int(marker & 0xFFFFFFFF)

	var ranges [][2]int
	// 回环
	if m.index < bufferIndex {
		ranges = append(ranges, [2]int{bufferIndex, len(m.buffers) - 1})
		ranges = append(ranges, [2]int{0, m.index})
	} else {
		ranges = append(ranges, [2]int{bufferIndex, m.index})
	}

	for _, ints := range ranges {
		for i := ints[0]; i <= ints[1]; i++ {

			for j := position; j < m.buffers[i].segments.Size(); j++ {
				cb(m.buffers[i].segments.Peek(j).data)
			}

			// 后续的切片, 从0开始
			position = 0
		}
	}
}

func (m *mergeWritingBuffer) Capacity() int {
	return m.segmentCount
}

func (m *mergeWritingBuffer) HasVideoDataInCurrentSegment() bool {
	return m.hasVideoDataInCurrentSegment
}

func NewMergeWritingBuffer(existVideo bool) MergeWritingBuffer {
	return &mergeWritingBuffer{
		startTS:    -1,
		existVideo: existVideo,
	}
}
