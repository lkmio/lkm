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
	FreeHead(size int)

	// FreeTail 从尾部释放指定大小内存
	FreeTail(size int)
}

func NewMemoryPool(capacity int) MemoryPool {
	pool := &memoryPool{
		data:     make([]byte, capacity),
		capacity: capacity,
	}

	return pool
}

type memoryPool struct {
	data     []byte
	ptrStart uintptr
	ptrEnd   uintptr
	//剩余的可用内存空间不足以为此次write
	capacity int
	head     int
	tail     int

	//保存开始索引
	mark int
}

// 根据head和tail计算出可用的内存地址
func (m *memoryPool) allocate(size int) []byte {
	if m.capacity-m.tail < size {
		//使用从头释放的内存
		if m.tail-m.mark+size <= m.head {
			copy(m.data, m.data[m.mark:m.tail])
			m.capacity = m.mark
			m.tail = m.tail - m.mark
			m.mark = 0
		} else {

			//扩容
			capacity := (cap(m.data) + m.tail - m.mark + size) * 3 / 2
			bytes := make([]byte, capacity)
			//不对之前的内存进行复制, 已经被AVPacket引用, 自行GC
			copy(bytes, m.data[m.mark:m.tail])
			m.data = bytes
			m.capacity = capacity
			m.tail = m.tail - m.mark
			m.mark = 0
			m.head = 0

		}
	}

	bytes := m.data[m.tail:]
	m.tail += size
	return bytes
}

func (m *memoryPool) Mark() {
	m.mark = m.tail
}

func (m *memoryPool) Write(data []byte) {
	allocate := m.allocate(len(data))
	copy(allocate, data)
}

func (m *memoryPool) Allocate(size int) []byte {
	return m.allocate(size)
}

func (m *memoryPool) Fetch() []byte {
	return m.data[m.mark:m.tail]
}

func (m *memoryPool) Reset() {
	m.tail = m.mark
}

func (m *memoryPool) FreeHead(size int) {
	m.head += size
	if m.head == m.tail {
		m.head = 0
		m.tail = 0
	} else if m.head >= m.capacity {
		m.head = 0
	}
}

func (m *memoryPool) FreeTail(size int) {
	m.tail -= size
	utils.Assert(m.tail >= 0)
}
