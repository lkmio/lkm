package stream

import (
	"encoding/hex"
	"testing"
)

func TestMemoryPool(t *testing.T) {
	bytes := make([]byte, 10)
	for i := 0; i < 10; i++ {
		bytes[i] = byte(i)
	}

	pool := NewMemoryPool(5)
	for i := 0; i < 10; i++ {
		pool.Mark()
		pool.Write(bytes)
		fetch := pool.Fetch()
		println(hex.Dump(fetch))

		if i%2 == 0 {
			pool.FreeHead(len(fetch))
		}
	}
}
