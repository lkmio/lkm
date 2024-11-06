package rtmp

import (
	"github.com/lkmio/avformat/libflv"
	"github.com/lkmio/avformat/librtmp"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"net"
)

// Publisher RTMP推流Source
type Publisher struct {
	stream.PublishSource

	stack *librtmp.Stack
}

func (p *Publisher) Input(data []byte) error {
	return p.stack.Input(nil, data)
}

func (p *Publisher) OnDeMuxStream(stream utils.AVStream) {
	// AVStream的ExtraData已经拷贝, 释放掉内存池中最新分配的内存
	p.FindOrCreatePacketBuffer(stream.Index(), stream.Type()).FreeTail()
	if !p.IsCompleted() {
		p.PublishSource.OnDeMuxStream(stream)
	} else if !p.IsTimeoutTrack(stream.Index()) {
		p.SetTimeoutTrack(stream.Index())
		log.Sugar.Errorf("添加 %s track超时", stream.Type().ToString())
	}
}

// OnVideo 解析出来的完整视频包
func (p *Publisher) OnVideo(index int, data []byte, ts uint32) {
	data = p.FindOrCreatePacketBuffer(index, utils.AVMediaTypeVideo).Fetch()
	// 交给flv解复用器, 解析出AVPacket
	p.PublishSource.TransDeMuxer.(libflv.DeMuxer).InputVideo(data, ts)
}

func (p *Publisher) OnAudio(index int, data []byte, ts uint32) {
	data = p.FindOrCreatePacketBuffer(index, utils.AVMediaTypeAudio).Fetch()
	p.PublishSource.TransDeMuxer.(libflv.DeMuxer).InputAudio(data, ts)
}

// OnPartPacket AVPacket的部分数据包
func (p *Publisher) OnPartPacket(index int, mediaType utils.AVMediaType, data []byte, first bool) {
	buffer := p.FindOrCreatePacketBuffer(index, mediaType)
	if first {
		buffer.Mark()
	}

	buffer.Write(data)
}

func (p *Publisher) Close() {
	p.PublishSource.Close()
	p.stack = nil
}

func NewPublisher(source string, stack *librtmp.Stack, conn net.Conn) *Publisher {
	deMuxer := libflv.NewDeMuxer()
	publisher := &Publisher{PublishSource: stream.PublishSource{ID: source, Type: stream.SourceTypeRtmp, TransDeMuxer: deMuxer, Conn: conn}, stack: stack}
	// 设置回调, 接受从DeMuxer解析出来的音视频包
	deMuxer.SetHandler(publisher)
	return publisher
}
