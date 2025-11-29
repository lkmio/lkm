package rtmp

import (
	"fmt"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/rtmp"
	"net"
	"time"
)

// Publisher RTMP推流Source
type Publisher struct {
	stream.PublishSource
	Stack *rtmp.ServerStack
}

func (p *Publisher) Close() {
	p.PublishSource.Close()
	p.Stack = nil
}

func NewPublisher(source string, stack *rtmp.ServerStack, conn net.Conn) *Publisher {
	demuxer := stack.FLV
	publisher := &Publisher{
		PublishSource: stream.PublishSource{
			ID:           source,
			SessionID:    fmt.Sprintf("%d", time.Now().UnixMilli()),
			Type:         stream.SourceTypeRtmp,
			TransDemuxer: demuxer,
			Conn:         conn},
		Stack: stack}

	// 设置回调, 接受从DeMuxer解析出来的音视频包
	demuxer.SetOnPreprocessPacketHandler(publisher.OnPreprocessPacket)
	demuxer.SetHandler(publisher)
	demuxer.AutoFree = false
	return publisher
}
