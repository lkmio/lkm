package stream

import "sync"

const (
	UDPReceiveBufferSize = 1500
	TCPReceiveBufferSize = 4096 * 20

	UDPReceiveBufferQueueSize = 1000
	TCPReceiveBufferQueueSize = 50
)

// 后续考虑使用cas队列实现
var (
	UDPReceiveBufferPool = sync.Pool{
		New: func() any {
			return make([]byte, UDPReceiveBufferSize)
		},
	}

	TCPReceiveBufferPool = sync.Pool{
		New: func() any {
			return make([]byte, TCPReceiveBufferSize)
		},
	}
)
