package stream

import (
	"github.com/yangjiechina/avformat/utils"
)

// MemoryPool 从解复用阶段，拼凑成完整的AVPacket开始(写)，到GOP缓存结束(释放)，整个过程都使用池中内存
// 类似环形缓冲区, 区别在于，写入的内存块是连续的、整块内存.
type MemoryPool interface {
	// Mark 标记一块写的内存地址
	//使用流程 Mark->Write/Allocate....->Fetch/Reset
	Mark()

	Write(data []byte)

	Allocate(size int) []byte

	Fetch() []byte

	// Reset 清空此次Write的标记，本次缓存的数据无效
	Reset()

	// FreeHead 从头部释放指定大小内存
	FreeHead()

	// FreeTail 从尾部释放指定大小内存
	FreeTail()
}

func NewMemoryPool(capacity int) MemoryPool {
	pool := &memoryPool{
		data:       make([]byte, capacity),
		capacity:   capacity,
		blockQueue: NewQueue(128),
	}

	return pool
}

type memoryPool struct {
	data []byte
	//实际的可用容量，当尾部剩余内存不足以此次Write, 并且头部有足够的空闲内存, 则尾部剩余的内存将不可用.
	capacity int
	head     int
	tail     int

	//保存开始索引
	markIndex  int
	mark       bool
	blockQueue *Queue
}

// 根据head和tail计算出可用的内存地址
func (m *memoryPool) allocate(size int) []byte {
	if m.capacity-m.tail < size {
		//使用从头释放的内存
		if m.tail-m.markIndex+size <= m.head {
			copy(m.data, m.data[m.markIndex:m.tail])
			m.capacity = m.markIndex
			m.tail = m.tail - m.markIndex
			m.markIndex = 0
		} else {

			//扩容
			capacity := (cap(m.data) + m.tail - m.markIndex + size) * 3 / 2
			bytes := make([]byte, capacity)
			//不对之前的内存进行复制, 已经被AVPacket引用, 自行GC
			copy(bytes, m.data[m.markIndex:m.tail])
			m.data = bytes
			m.capacity = capacity
			m.tail = m.tail - m.markIndex
			m.markIndex = 0
			m.head = 0

		}
	}

	bytes := m.data[m.tail:]
	m.tail += size
	return bytes
}

func (m *memoryPool) Mark() {
	m.markIndex = m.tail
	m.mark = true
}

func (m *memoryPool) Write(data []byte) {
	utils.Assert(m.mark)

	allocate := m.allocate(len(data))
	copy(allocate, data)
}

func (m *memoryPool) Allocate(size int) []byte {
	utils.Assert(m.mark)
	return m.allocate(size)
}

func (m *memoryPool) Fetch() []byte {
	utils.Assert(m.mark)

	m.mark = false
	size := m.tail - m.markIndex
	m.blockQueue.Push(size)
	return m.data[m.markIndex:m.tail]
}

func (m *memoryPool) Reset() {
	m.mark = false
	m.tail = m.markIndex
}

func (m *memoryPool) FreeHead() {
	utils.Assert(!m.blockQueue.IsEmpty())

	size := m.blockQueue.Pop().(int)
	m.head += size

	if m.head == m.tail {
		m.head = 0
		m.tail = 0
	} else if m.head >= m.capacity {
		m.head = 0
	}
}

func (m *memoryPool) FreeTail() {
	utils.Assert(!m.blockQueue.IsEmpty())

	size := m.blockQueue.PopBack().(int)
	m.tail -= size

	if m.tail == 0 && !m.blockQueue.IsEmpty() {
		m.tail = m.capacity
	}
}
