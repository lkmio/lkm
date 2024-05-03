package stream

import (
	"github.com/yangjiechina/avformat/utils"
)

// MemoryPool 从解复用阶段，拼凑成完整的AVPacket开始(写)，到GOP缓存结束(释放)，整个过程都使用池中内存
// 类似环形缓冲区, 区别在于，写入的内存块是连续的、整块内存.
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

	// Reset 清空写入的数据，本次缓存的数据无效
	Reset()

	// Reserve 保留指定大小的内存空间
	//主要是为了和实现和Write相似功能，但是不拷贝, 所以使用流程和Write一样.
	Reserve(size int)

	// FreeHead 从头部释放指定大小内存
	FreeHead()

	// FreeTail 从尾部释放指定大小内存
	FreeTail()

	Data() ([]byte, []byte)

	Clear()

	Empty() bool

	Capacity() int

	Size() int
}

func NewMemoryPool(capacity int) MemoryPool {
	pool := &memoryPool{
		data:       make([]byte, capacity),
		capacity:   capacity,
		blockQueue: NewQueue(128),
	}

	return pool
}

func NewMemoryPoolWithRecopy(capacity int) MemoryPool {
	pool := &memoryPool{
		data:       make([]byte, capacity),
		capacity:   capacity,
		blockQueue: NewQueue(128),
		recopy:     true,
	}

	return pool
}

func NewMemoryPoolWithDirect(capacity int, recopy bool) MemoryPool {
	pool := &memoryPool{
		data:       make([]byte, capacity),
		capacity:   capacity,
		blockQueue: NewQueue(128),
		recopy:     recopy,
		direct:     true,
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

	recopy bool
	direct bool
}

// 根据head和tail计算出可用的内存地址
func (m *memoryPool) allocate(size int) []byte {
	if m.capacity-m.tail < size {
		//使用从头释放的内存
		if !m.direct && m.tail-m.markIndex+size <= m.head {
			copy(m.data, m.data[m.markIndex:m.tail])
			m.capacity = m.markIndex
			m.tail = m.tail - m.markIndex
			m.markIndex = 0
		} else {
			//扩容
			writeSize := m.tail - m.markIndex
			capacity := (cap(m.data) + writeSize + size) * 2
			bytes := make([]byte, capacity)

			if m.recopy {
				//将扩容前的老数据复制到新的内存空间
				head, tail := m.Data()
				copy(bytes, head)
				copy(bytes[len(head):], tail)
				m.tail = len(head) + len(tail)
				m.markIndex = m.tail - writeSize
			} else {
				//不对之前的内存进行复制, 已经被AVPacket引用, 自行GC
				copy(bytes, m.data[m.markIndex:m.tail])
				m.tail = writeSize
				m.markIndex = 0
			}

			m.data = bytes
			m.capacity = capacity
			m.head = 0
		}
	}

	bytes := m.data[m.tail:]
	m.tail += size
	return bytes
}

func (m *memoryPool) Mark() {
	utils.Assert(!m.mark)

	m.markIndex = m.tail
	m.mark = true
}

func (m *memoryPool) Write(data []byte) {
	utils.Assert(m.mark)

	allocate := m.allocate(len(data))
	copy(allocate, data)
}

func (m *memoryPool) Reserve(size int) {
	utils.Assert(m.mark)
	_ = m.allocate(size)
}

func (m *memoryPool) Allocate(size int) []byte {
	m.Mark()
	_ = m.allocate(size)
	return m.Fetch()
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
	utils.Assert(!m.mark)
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
	utils.Assert(!m.mark)
	utils.Assert(!m.blockQueue.IsEmpty())

	size := m.blockQueue.PopBack().(int)
	m.tail -= size
	if m.tail == 0 && !m.blockQueue.IsEmpty() {
		m.tail = m.capacity
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
	m.mark = false

	m.blockQueue.Clear()
}

func (m *memoryPool) Empty() bool {
	utils.Assert(!m.mark)
	return m.blockQueue.Size() < 1
}

func (m *memoryPool) Capacity() int {
	return m.capacity
}

func (m *memoryPool) Size() int {
	head, tail := m.Data()
	return len(head) + len(tail)
}
