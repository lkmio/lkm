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
