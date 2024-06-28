package stream

import (
	"github.com/yangjiechina/avformat/utils"
)

// MemoryPool 从解复用阶段，拼凑成完整的AVPacket开始(写)，到GOP缓存结束(释放)，整个过程都使用池中内存
// 类似环形缓冲区, 区别在于，内存块是连续的、整块内存.
// AVPacket缓存使用memorypool_rb, 允许回环(内存必须完整). tranStream使用memorypool_direct, 连续一块完整的内存, 否则与合并缓存写的观念背道而驰.
// 两种使用方式:
//  1. 已知需要分配内存大小， 直接使用Allocate()函数分配, 并且外部自行操作内存块
//  2. 未知分配内存大小, 先使用Mark()函数，标记内存起始偏移量, 再通过Write()函数将数据拷贝进内存块，最后调用Fetch/Reset函数完成或释放内存块
//
// 两种使用方式互斥，不能同时使用.
type MemoryPool interface {
	// Allocate 分配指定大小的内存块
	Allocate(size int) []byte

	// Mark 标记内存块起始位置
	Mark()

	// Write 向内存块中写入数据, 必须先调用Mark函数
	Write(data []byte)

	// Fetch 获取当前内存块，必须先调用Mark函数
	Fetch() []byte

	// Reset 清空本次流程写入的还未生效内存块
	Reset()

	// Reserve 预留指定大小的内存块
	//主要是为了和实现和Write相似功能，但是不拷贝, 所以使用流程和Write一样.
	Reserve(size int)

	// FreeHead 释放头部一块内存
	FreeHead()

	// FreeTail 释放尾部一块内存
	FreeTail()

	// Data 返回头尾已使用的内存块
	Data() ([]byte, []byte)

	// Clear 清空所有内存块
	Clear()
}

type memoryPool struct {
	data     []byte
	capacity int //实际的可用容量，当尾部剩余内存不足以此次Write, 并且头部有足够的空闲内存, 则尾部剩余的内存将不可用.
	head     int //起始索引
	tail     int //末尾索引, 当形成回环时, 会小于起始索引

	markIndex         int //分配内存块的起始索引, 一定小于末尾索引, data[markIndex:tail]此次分配的内存块
	marked            bool
	blockQueue        *Queue
	discardBlockCount int  //扩容时, 丢弃之前的内存块数量
	recopy            bool //扩容时，是否拷贝旧数据. 缓存AVPacket时, 内存已经被Data引用，所以不需要再拷贝旧数据. 用作合并写缓存时, 流还没有发送使用, 需要拷贝旧数据.
	isFull            func(int) bool
}

func (m *memoryPool) grow(size int) {
	//1.5倍扩容
	newData := make([]byte, (cap(m.data)+size)*3/2)
	//未写入缓冲区大小
	flushSize := m.tail - m.markIndex
	//拷贝之前的数据
	if m.recopy {
		head, tail := m.Data()
		copy(newData, head)
		copy(newData[len(head):], tail)

		m.head = 0
		m.tail = len(head) + len(tail)
		m.markIndex = m.tail - flushSize
	} else {
		//只拷贝本回合数据
		copy(newData, m.data[m.tail-flushSize:m.tail])
		//丢弃之前的内存块
		m.discardBlockCount += m.blockQueue.Size()
		m.blockQueue.Clear()

		m.head = 0
		m.tail = flushSize
		m.markIndex = 0
	}

	m.data = newData
	m.capacity = cap(newData)
}

// 根据head和tail计算出可用的内存地址
func (m *memoryPool) allocate(size int) []byte {
	if m.isFull(size) {
		m.grow(size)
	}

	bytes := m.data[m.tail : m.tail+size]
	m.tail += size
	return bytes
}

func (m *memoryPool) Mark() {
	utils.Assert(!m.marked)

	m.markIndex = m.tail
	m.marked = true
}

func (m *memoryPool) Write(data []byte) {
	utils.Assert(m.marked)

	allocate := m.allocate(len(data))
	copy(allocate, data)
}

func (m *memoryPool) Reserve(size int) {
	utils.Assert(m.marked)
	_ = m.allocate(size)
}

func (m *memoryPool) Allocate(size int) []byte {
	m.Mark()
	_ = m.allocate(size)
	return m.Fetch()
}

func (m *memoryPool) Fetch() []byte {
	utils.Assert(m.marked)

	m.marked = false
	size := m.tail - m.markIndex
	m.blockQueue.Push(size)
	return m.data[m.markIndex:m.tail]
}

func (m *memoryPool) Reset() {
	m.marked = false
	m.tail = m.markIndex
}

func (m *memoryPool) freeOldBlocks() bool {
	utils.Assert(!m.marked)

	if m.discardBlockCount > 0 {
		m.discardBlockCount--
		return true
	}

	return false
}

func (m *memoryPool) FreeHead() {
	if m.freeOldBlocks() {
		return
	}

	utils.Assert(!m.blockQueue.IsEmpty())
	size := m.blockQueue.Pop().(int)
	m.head += size

	if m.blockQueue.IsEmpty() {
		m.Clear()
	} else if m.head >= m.capacity {
		//清空末尾, 从头开始
		m.head = 0
	}
}

func (m *memoryPool) FreeTail() {
	if m.freeOldBlocks() {
		return
	}

	utils.Assert(!m.blockQueue.IsEmpty())
	size := m.blockQueue.PopBack().(int)
	m.tail -= size

	if m.blockQueue.IsEmpty() {
		m.Clear()
	} else if m.tail == 0 {
		//回环回到线性
		m.tail = m.capacity
		m.capacity = cap(m.data)
	}
}

func (m *memoryPool) Data() ([]byte, []byte) {
	if m.tail <= m.head && !m.blockQueue.IsEmpty() {
		return m.data[m.head:m.capacity], m.data[:m.tail]
	} else {
		return m.data[m.head:m.tail], nil
	}
}

func (m *memoryPool) Clear() {
	m.capacity = cap(m.data)
	m.head = 0
	m.tail = 0

	m.markIndex = 0
	m.marked = false

	m.blockQueue.Clear()
	m.discardBlockCount = 0
}
