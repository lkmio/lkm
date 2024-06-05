package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/stream"
	"net"
)

// Publisher RTMP推流Source
type Publisher struct {
	stream.SourceImpl

	stack *librtmp.Stack
}

func NewPublisher(sourceId string, stack *librtmp.Stack, conn net.Conn) *Publisher {
	deMuxer := libflv.NewDeMuxer(libflv.TSModeRelative)
	publisher_ := &Publisher{SourceImpl: stream.SourceImpl{Id_: sourceId, Type_: stream.SourceTypeRtmp, TransDeMuxer: deMuxer, Conn: conn}, stack: stack}
	//设置回调，从flv解析出来的Stream和AVPacket都将统一回调到stream.SourceImpl
	deMuxer.SetHandler(publisher_)
	//为推流方分配足够多的缓冲区
	conn.(*transport.Conn).ReallocateRecvBuffer(1024 * 1024)
	return publisher_
}

func (p *Publisher) Input(data []byte) error {
	return p.stack.Input(nil, data)
}

func (p *Publisher) OnDeMuxStream(stream utils.AVStream) {
	//AVStream的ExtraData已经拷贝, 释放掉内存池中最新分配的内存
	p.FindOrCreatePacketBuffer(stream.Index(), stream.Type()).FreeTail()
	p.SourceImpl.OnDeMuxStream(stream)
}

// OnVideo 解析出来的完整视频包
// @ts 	   rtmp chunk的相对时间戳
func (p *Publisher) OnVideo(index int, data []byte, ts uint32) {
	data = p.FindOrCreatePacketBuffer(index, utils.AVMediaTypeVideo).Fetch()
	//交给flv解复用器, 解析回调出AVPacket
	p.SourceImpl.TransDeMuxer.(libflv.DeMuxer).InputVideo(data, ts)
}

func (p *Publisher) OnAudio(index int, data []byte, ts uint32) {
	data = p.FindOrCreatePacketBuffer(index, utils.AVMediaTypeAudio).Fetch()
	p.SourceImpl.TransDeMuxer.(libflv.DeMuxer).InputAudio(data, ts)
}

func (p *Publisher) OnPartPacket(index int, mediaType utils.AVMediaType, data []byte, first bool) {
	buffer := p.FindOrCreatePacketBuffer(index, mediaType)
	if first {
		buffer.Mark()
	}

	buffer.Write(data)
}
