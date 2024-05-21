package stream

import (
	"encoding/hex"
	"github.com/yangjiechina/avformat/utils"
	"testing"
	"unsafe"
)

func TestMemoryPool(t *testing.T) {
	bytes := make([]byte, 10)
	for i := 0; i < 10; i++ {
		bytes[i] = byte(i)
	}

	pool := NewDirectMemoryPool(5)
	last := uintptr(0)
	for i := 0; i < 10; i++ {
		pool.Mark()
		pool.Write(bytes)
		fetch := pool.Fetch()
		addr := *(*uintptr)(unsafe.Pointer(&fetch))
		if last != 0 {
			utils.Assert(last == addr)
		}
		last = addr

		println(hex.Dump(fetch))

		pool.FreeTail()
	}
}
