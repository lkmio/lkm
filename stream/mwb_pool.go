package stream

import (
	"fmt"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/log"
	"sync"
)

const (
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

	pendingReleaseBuffers = make(map[string]*collections.Queue[*mbBuffer])
	lock                  sync.Mutex
)

func AddMWBuffersToPending(sourceId string, transStreamId TransStreamID, buffers *collections.Queue[*mbBuffer]) {
	key := fmt.Sprintf("%s-%d", sourceId, transStreamId)

	lock.Lock()
	defer lock.Unlock()

	for buffers.Size() > 0 {
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

		pendingReleaseBuffers[key] = buffers
	}
}

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

func release(buffers *collections.Queue[*mbBuffer], length int) bool {
	var count int
	for i := 0; i < length; i++ {
		buffer := buffers.Peek(i)
		size := buffer.segments.Size()

		var j int
		for ; j < size; j++ {
			segment := buffer.segments.Peek(0)
			if segment.UseCount() > 1 {
				break
			}

			buffer.segments.Pop()
		}

		// 所有切片都已经没有使用, 释放内存池
		if j == size {
			buffer.buffer.Clear()
			MWBufferPool.Put(buffer)
			count++
		}
	}

	for ; count > 0; count-- {
		buffers.Pop()
	}

	return buffers.Size() == 0
}
