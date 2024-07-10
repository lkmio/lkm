package collections

type directMemoryPool struct {
	*memoryPool
}

func (m *directMemoryPool) isFull(size int) bool {
	//尾部没有大小合适的内存空间
	return m.capacity-m.tail < size
}

func NewDirectMemoryPool(capacity int) MemoryPool {
	pool := &directMemoryPool{}
	pool.memoryPool = &memoryPool{
		data:       make([]byte, capacity),
		capacity:   capacity,
		blockQueue: NewQueue(2048),
		recopy:     true,
		isFull:     pool.isFull,
	}

	return pool
}
