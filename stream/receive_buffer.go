package stream

const (
	ReceiveBufferUdpBlockCount = 300

	ReceiveBufferTCPBlockCount = 50
)

// ReceiveBuffer 收流缓冲区. 网络收流->解析流->封装流->发送流是同步的,从解析到发送可能耗时,从而影响读取网络流. 使用收流缓冲区,可有效降低出现此问题的概率.
// 从网络IO读取数据->送给解复用器, 此过程需做到无内存拷贝
// rtmp和1078推流直接使用ReceiveBuffer
// 国标推流,UDP收流都要经过jitter buffer处理, 还是需要拷贝一次, 没必要使用ReceiveBuffer. TCP全都使用ReceiveBuffer, 区别在于多端口模式, 第一包传给source, 单端口模式先解析出ssrc, 找到source. 后续再传给source.
type ReceiveBuffer struct {
	blockCapacity int    // 单个内存块的容量
	blockCount    int    // 内存块数量
	data          []byte // 由一块大内存分割成多个块使用
	index         int    // 使用到第几块的索引
}

func (r *ReceiveBuffer) Index() int {
	return r.index
}

func (r *ReceiveBuffer) Get(index int) []byte {
	return r.data[index*r.blockCapacity : (index+1)*r.blockCapacity]
}

func (r *ReceiveBuffer) GetBlock() []byte {
	bytes := r.data[r.index*r.blockCapacity:]
	r.index = (r.index + 1) % r.blockCount
	return bytes[:r.blockCapacity]
}

func (r *ReceiveBuffer) BlockCount() int {
	return r.blockCount
}

func NewReceiveBuffer(blockSize, blockCount int) *ReceiveBuffer {
	return &ReceiveBuffer{blockCapacity: blockSize, blockCount: blockCount, data: make([]byte, blockSize*blockCount), index: 0}
}

func NewUDPReceiveBuffer() *ReceiveBuffer {
	return NewReceiveBuffer(1500, ReceiveBufferUdpBlockCount)
}

func NewTCPReceiveBuffer() *ReceiveBuffer {
	return NewReceiveBuffer(4096*20, ReceiveBufferTCPBlockCount)
}
