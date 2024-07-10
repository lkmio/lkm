package stream

import (
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/collections"
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

type mergeWritingBuffer struct {
	mwBlocks []collections.MemoryPool

	//空闲合并写块
	freeKeyFrameMWBlocks     collections.LinkedList[collections.MemoryPool]
	freeNoneKeyFrameMWBlocks collections.LinkedList[collections.MemoryPool]

	index    int   //当前切片位于mwBlocks的索引
	startTS  int64 //当前切片的开始时间
	duration int   //当前切片时长

	lastKeyFrameIndex int  //最新关键帧所在切片的索引
	existVideo        bool //是否存在视频

	keyFrameBufferMaxLength    int
	nonKeyFrameBufferMaxLength int
	keyFrameMap                map[int]int
}

func (m *mergeWritingBuffer) createMWBlock(videoKey bool) collections.MemoryPool {
	if videoKey {
		return collections.NewDirectMemoryPool(m.keyFrameBufferMaxLength)
	} else {
		return collections.NewDirectMemoryPool(m.nonKeyFrameBufferMaxLength)
	}
}

func (m *mergeWritingBuffer) grow() {
	pools := make([]collections.MemoryPool, cap(m.mwBlocks)*3/2)
	for i := 0; i < cap(m.mwBlocks); i++ {
		pools[i] = m.mwBlocks[i]
	}

	m.mwBlocks = pools
}

func (m *mergeWritingBuffer) Allocate(size int, ts int64, videoKey bool) []byte {
	if !AppConfig.GOPCache || !m.existVideo {
		return m.mwBlocks[0].Allocate(size)
	}

	utils.Assert(ts != -1)

	//新的切片
	if m.startTS == -1 {
		if _, ok := m.keyFrameMap[m.index]; ok {
			delete(m.keyFrameMap, m.index)
		}

		if m.mwBlocks[m.index] == nil {
			//创建内存块
			m.mwBlocks[m.index] = m.createMWBlock(videoKey)
		} else {
			//循环使用
			if !videoKey {

				//I帧间隔长, 不够写一组GOP, 扩容!
				if len(m.keyFrameMap) < 1 {
					capacity := len(m.mwBlocks)
					m.grow()
					m.index = capacity

					m.mwBlocks[m.index] = m.createMWBlock(videoKey)
				}
			}

			m.mwBlocks[m.index].Clear()
		}

		m.startTS = ts
	}

	if videoKey {
		//请务必确保关键帧帧从新的切片开始
		//外部遇到关键帧请先调用FlushSegment
		utils.Assert(m.mwBlocks[m.index].IsEmpty())
		m.lastKeyFrameIndex = m.index
		m.keyFrameMap[m.index] = m.index
	}

	if ts < m.startTS {
		m.startTS = ts
	}

	m.duration = int(ts - m.startTS)
	return m.mwBlocks[m.index].Allocate(size)
}

func (m *mergeWritingBuffer) FlushSegment() []byte {
	if m.mwBlocks[m.index] == nil {
		return nil
	}

	data, _ := m.mwBlocks[m.index].Data()
	if len(data) == 0 {
		return nil
	}

	//更新缓冲长度
	if m.lastKeyFrameIndex == m.index && m.keyFrameBufferMaxLength < len(data) {
		m.keyFrameBufferMaxLength = len(data) * 3 / 2
	} else if m.lastKeyFrameIndex != m.index && m.nonKeyFrameBufferMaxLength < len(data) {
		m.nonKeyFrameBufferMaxLength = len(data) * 3 / 2
	}

	m.index = (m.index + 1) % cap(m.mwBlocks)
	m.startTS = -1
	m.duration = 0
	return data
}

func (m *mergeWritingBuffer) PeekCompletedSegment() []byte {
	if !AppConfig.GOPCache || !m.existVideo {
		data, _ := m.mwBlocks[0].Data()
		m.mwBlocks[0].Clear()
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
	if m.mwBlocks[m.index] == nil {
		return true
	}

	data, _ := m.mwBlocks[m.index].Data()
	return len(data) == 0
}

func (m *mergeWritingBuffer) Reserve(number int) {
	utils.Assert(m.mwBlocks[m.index] != nil)

	m.mwBlocks[m.index].Reserve(number)
}

func (m *mergeWritingBuffer) ReadSegmentsFromKeyFrameIndex(cb func([]byte)) {
	if m.lastKeyFrameIndex < 0 || m.index == m.lastKeyFrameIndex {
		return
	}

	for i := m.lastKeyFrameIndex; i < cap(m.mwBlocks); i++ {
		if m.mwBlocks[i] == nil {
			continue
		}

		data, _ := m.mwBlocks[i].Data()
		cb(data)
	}

	//回调循环使用的头部数据
	if m.index < m.lastKeyFrameIndex {
		for i := 0; i < m.index; i++ {
			data, _ := m.mwBlocks[i].Data()
			cb(data)
		}
	}
}

func NewMergeWritingBuffer(existVideo bool) MergeWritingBuffer {
	//开启GOP缓存, 输出流也缓存整个GOP
	var blocks []collections.MemoryPool
	if existVideo {
		blocks = make([]collections.MemoryPool, AppConfig.WriteBufferNumber)
	} else {
		blocks = make([]collections.MemoryPool, 1)
	}

	if !existVideo || !AppConfig.GOPCache {
		blocks[0] = collections.NewDirectMemoryPool(1024 * 100)
	}

	return &mergeWritingBuffer{
		keyFrameBufferMaxLength:    AppConfig.MergeWriteLatency * 1024 * 2,
		nonKeyFrameBufferMaxLength: AppConfig.MergeWriteLatency * 1024 / 2,
		mwBlocks:                   blocks,
		startTS:                    -1,
		lastKeyFrameIndex:          -1,
		existVideo:                 existVideo,
		keyFrameMap:                make(map[int]int, 5),
	}
}
