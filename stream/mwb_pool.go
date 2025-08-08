package stream

import (
	"fmt"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/log"
	"sync"
)

const (
	// BlockBufferSize 合并写缓冲区的内存块大小
	// 一块缓冲区可以包含多个合并写切片
	BlockBufferSize = 1024 * 1024 * 2
)

var (
	MWBufferPool = sync.Pool{
		New: func() any {
			log.Sugar.Debug("create new merge writing buffer")

			return &mbBuffer{
				buffer:   collections.NewDirectBlockBuffer(BlockBufferSize),
				segments: collections.NewQueue[*collections.ReferenceCounter[[]byte]](32),
			}
		},
	}

	pendingReleaseBuffers = make(map[string]*collections.Queue[*mbBuffer]) // 等待释放的合并写缓冲区
	lock                  sync.Mutex
)

// AddMWBuffersToPending 添加合并写缓冲区到等待释放队列
func AddMWBuffersToPending(sourceId string, transStreamId TransStreamID, buffers *collections.Queue[*mbBuffer]) {
	key := fmt.Sprintf("%s-%d", sourceId, transStreamId)

	lock.Lock()
	defer lock.Unlock()

	v, ok := pendingReleaseBuffers[key]
	if ok {
		// 第二次都推流结束了，第一次的内存还被占用
		// 强制释放上次推流的内存池
		log.Sugar.Warnf("force release last pending buffers of %s", key)

		for v.Size() > 0 {
			pop := v.Pop()
			pop.buffer.Clear()
			pop.segments.Clear()
			MWBufferPool.Put(pop)
		}

		delete(pendingReleaseBuffers, key)
	}

	if buffers.Size() > 0 {
		pendingReleaseBuffers[key] = buffers
	}
}

// ReleasePendingBuffers 释放等待释放的合并写缓冲区
// 拉流结束后主动调用一次, 创建传输流的时候也调用一次
func ReleasePendingBuffers(sourceId string, transStreamId TransStreamID) {
	key := fmt.Sprintf("%s-%d", sourceId, transStreamId)

	lock.Lock()
	defer lock.Unlock()

	v, ok := pendingReleaseBuffers[key]
	if !ok || !release(v, v.Size()) {
		return
	}

	delete(pendingReleaseBuffers, key)
}

// release 释放合并写缓冲区
func release(buffers *collections.Queue[*mbBuffer], length int) bool {
	for i := 0; i < length; i++ {
		buffer := buffers.Peek(0)
		size := buffer.segments.Size()

		// 判断最后一个合并写切片是否已经释放, 最后一个都释放了，前面的肯定也已经释放了
		if size == 0 || (size > 0 && buffer.segments.Peek(size-1).UseCount() < 2) {
			buffers.Pop()
			buffer.buffer.Clear()
			buffer.segments.Clear()
			MWBufferPool.Put(buffer)
		} else {
			break
		}
	}
	return buffers.Size() == 0
}
