package stream

type rbMemoryPool struct {
	*memoryPool
}

func (m *rbMemoryPool) isFull(size int) bool {
	//已经回环
	over := m.tail < m.head
	if over && m.head-m.tail >= size {
		//头部有大小合适的内存空间
	} else if !over && m.capacity-m.tail >= size {
		//尾部有大小合适的内存空间
	} else if !over && m.head > size {
		//形成回环

		//修改有效内存容量大小
		m.capacity = m.markIndex
		//拷贝之前的数据
		incompleteBlockSize := m.tail - m.markIndex
		copy(m.data, m.data[m.markIndex:m.tail])
		m.markIndex = 0
		m.tail = incompleteBlockSize
	} else {
		return true
	}

	return false
}

func NewRbMemoryPool(capacity int) MemoryPool {
	pool := &rbMemoryPool{}
	pool.memoryPool = &memoryPool{
		data:       make([]byte, capacity),
		capacity:   capacity,
		blockQueue: NewQueue(2048),
		recopy:     false,
		isFull:     pool.isFull,
	}

	return pool
}
