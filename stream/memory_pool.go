package stream

// MemoryPool
// 从解复用阶段，拼凑成完整的AVPacket开始(写)，到GOP缓存结束(释放)，整个过程都使用池中内存
type MemoryPool interface {
	Allocate(size int) []byte

	Free(size int)
}

func NewMemoryPool(capacity int) MemoryPool {
	pool := &memoryPool{
		data: make([]byte, capacity),
	}

	return pool
}

type memoryPool struct {
	data []byte
	size int
}

func (m *memoryPool) Allocate(size int) []byte {
	return nil
}

func (m *memoryPool) Free(size int) {
}
