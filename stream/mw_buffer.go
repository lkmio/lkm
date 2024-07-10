package stream

import (
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/collections"
)

// MergeWritingBuffer 实现针对RTMP/FLV/HLS等基于TCP传输流的合并写缓存
// 包含多个合并写块, 循环使用, 至少需要等到第二个I帧才开始循环. webrtcI帧间隔可能会高达几十秒,
// 容量根据write_timeout发送超时和合并写时间来计算, write_timeout/mw_latency.如果I帧间隔大于发送超时时间, 则需要创建新的块.
type MergeWritingBuffer interface {
	Allocate(size int, ts int64, videoKey bool) []byte

	// PeekCompletedSegment 返回当前完整切片, 如果不满, 返回nil.
	PeekCompletedSegment() []byte

	// FlushSegment 保存当前切片, 创建新的切片
	FlushSegment() []byte

	IsFull(ts int64) bool

	// IsNewSegment 新切片, 还未写数据
	IsNewSegment() bool

	// Reserve 从当前切片中预留指定长度数据
	Reserve(number int)

	// ReadSegmentsFromKeyFrameIndex 从最近的关键帧读取切片
	ReadSegmentsFromKeyFrameIndex(cb func([]byte))
}

type mwBlock struct {
	free     bool
	keyVideo bool
	buffer   collections.MemoryPool
}

type mergeWritingBuffer struct {
	mwBlocks []mwBlock

	//空闲合并写块
	keyFrameFreeMWBlocks     collections.LinkedList[collections.MemoryPool]
	noneKeyFreeFrameMWBlocks collections.LinkedList[collections.MemoryPool]

	index    int   //当前切片位于mwBlocks的索引
	startTS  int64 //当前切片的开始时间
	duration int   //当前切片时长

	lastKeyFrameIndex int  //最新关键帧所在切片的索引
	keyFrameCount     int  //关键帧计数
	existVideo        bool //是否存在视频

	keyFrameBufferMaxLength    int
	nonKeyFrameBufferMaxLength int
}

func (m *mergeWritingBuffer) createMWBlock(videoKey bool) mwBlock {
	if videoKey {
		return mwBlock{true, videoKey, collections.NewDirectMemoryPool(m.keyFrameBufferMaxLength)}
	} else {
		return mwBlock{true, false, collections.NewDirectMemoryPool(m.nonKeyFrameBufferMaxLength)}
	}
}

func (m *mergeWritingBuffer) grow() {
	pools := make([]mwBlock, cap(m.mwBlocks)*3/2)
	for i := 0; i < cap(m.mwBlocks); i++ {
		pools[i] = m.mwBlocks[i]
	}

	m.mwBlocks = pools
}

func (m *mergeWritingBuffer) Allocate(size int, ts int64, videoKey bool) []byte {
	if !AppConfig.GOPCache || !m.existVideo {
		return m.mwBlocks[0].buffer.Allocate(size)
	}

	utils.Assert(ts != -1)

	//新的切片
	if m.startTS == -1 {
		m.startTS = ts

		if m.mwBlocks[m.index].buffer == nil {
			//创建内存块
			m.mwBlocks[m.index] = m.createMWBlock(videoKey)
		} else {
			//循环使用
			m.mwBlocks[m.index].buffer.Clear()

			if m.mwBlocks[m.index].keyVideo {
				m.keyFrameCount--
			}
		}

		m.mwBlocks[m.index].free = false
		m.mwBlocks[m.index].keyVideo = videoKey
	}

	if videoKey {
		//请务必确保关键帧帧从新的切片开始
		//外部遇到关键帧请先调用FlushSegment
		utils.Assert(m.mwBlocks[m.index].buffer.IsEmpty())
		m.lastKeyFrameIndex = m.index
		m.keyFrameCount++
	}

	if ts < m.startTS {
		m.startTS = ts
	}

	m.duration = int(ts - m.startTS)
	return m.mwBlocks[m.index].buffer.Allocate(size)
}

func (m *mergeWritingBuffer) FlushSegment() []byte {
	if !AppConfig.GOPCache || !m.existVideo {
		return nil
	} else if m.mwBlocks[m.index].buffer == nil || m.mwBlocks[m.index].free {
		return nil
	}

	data, _ := m.mwBlocks[m.index].buffer.Data()
	if len(data) == 0 {
		return nil
	}

	//更新缓冲长度
	if m.lastKeyFrameIndex == m.index && m.keyFrameBufferMaxLength < len(data) {
		m.keyFrameBufferMaxLength = len(data) * 3 / 2
	} else if m.lastKeyFrameIndex != m.index && m.nonKeyFrameBufferMaxLength < len(data) {
		m.nonKeyFrameBufferMaxLength = len(data) * 3 / 2
	}

	capacity := cap(m.mwBlocks)
	if m.index+1 == capacity && m.keyFrameCount == 1 {
		m.grow()
	}

	m.index = (m.index + 1) % capacity
	m.startTS = -1
	m.duration = 0
	m.mwBlocks[m.index].free = true
	return data
}

func (m *mergeWritingBuffer) PeekCompletedSegment() []byte {
	if !AppConfig.GOPCache || !m.existVideo {
		data, _ := m.mwBlocks[0].buffer.Data()
		m.mwBlocks[0].buffer.Clear()
		return data
	}

	if m.duration < AppConfig.MergeWriteLatency {
		return nil
	}

	return m.FlushSegment()
}

func (m *mergeWritingBuffer) IsFull(ts int64) bool {
	if m.startTS == -1 {
		return false
	}

	return int(ts-m.startTS) >= AppConfig.MergeWriteLatency
}

func (m *mergeWritingBuffer) IsNewSegment() bool {
	return m.mwBlocks[m.index].buffer == nil || m.mwBlocks[m.index].free
}

func (m *mergeWritingBuffer) Reserve(number int) {
	utils.Assert(m.mwBlocks[m.index].buffer != nil)

	m.mwBlocks[m.index].buffer.Reserve(number)
}

func (m *mergeWritingBuffer) ReadSegmentsFromKeyFrameIndex(cb func([]byte)) {
	if m.keyFrameCount == 0 {
		return
	}

	for i := m.lastKeyFrameIndex; i < cap(m.mwBlocks); i++ {
		if m.mwBlocks[i].buffer == nil {
			continue
		}

		data, _ := m.mwBlocks[i].buffer.Data()
		cb(data)
	}

	//回调循环使用的头部数据
	if m.index < m.lastKeyFrameIndex {
		for i := 0; i < m.index; i++ {
			data, _ := m.mwBlocks[i].buffer.Data()
			cb(data)
		}
	}
}

func NewMergeWritingBuffer(existVideo bool) MergeWritingBuffer {
	//开启GOP缓存, 输出流也缓存整个GOP
	var blocks []mwBlock
	if existVideo {
		blocks = make([]mwBlock, AppConfig.WriteBufferNumber)
	} else {
		blocks = make([]mwBlock, 1)
	}

	if !existVideo || !AppConfig.GOPCache {
		blocks[0] = mwBlock{true, false, collections.NewDirectMemoryPool(1024 * 100)}
	}

	return &mergeWritingBuffer{
		keyFrameBufferMaxLength:    AppConfig.MergeWriteLatency * 1024 * 2,
		nonKeyFrameBufferMaxLength: AppConfig.MergeWriteLatency * 1024 / 2,
		mwBlocks:                   blocks,
		startTS:                    -1,
		lastKeyFrameIndex:          -1,
		existVideo:                 existVideo,
	}
}
