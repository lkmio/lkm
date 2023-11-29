package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

type Publisher struct {
	stream.SourceImpl

	audioMemoryPool stream.MemoryPool
	videoMemoryPool stream.MemoryPool

	audioUnmark bool
	videoUnmark bool
}

func NewPublisher(sourceId string) *Publisher {
	deMuxer := libflv.NewDeMuxer()
	publisher := &Publisher{SourceImpl: stream.SourceImpl{Id_: sourceId, TransDeMuxer: nil}, audioUnmark: false, videoUnmark: false}
	//设置回调，从flv解析出来的Stream和AVPacket都将统一回调到stream.SourceImpl
	deMuxer.SetHandler(publisher)
	publisher.SourceImpl.SetState(stream.SessionStateTransferring)

	//创建内存池
	publisher.audioMemoryPool = stream.NewMemoryPool(48000 * (stream.AppConfig.GOPCache + 1))
	if stream.AppConfig.GOPCache > 0 {
		//以每秒钟4M码率大小创建内存池
		publisher.videoMemoryPool = stream.NewMemoryPool(4096 * 1000 / 8 * stream.AppConfig.GOPCache)
	} else {
		publisher.videoMemoryPool = stream.NewMemoryPool(4096 * 1000 / 8)
	}
	return publisher
}

func (p *Publisher) OnDiscardPacket(pkt interface{}) {
	packet := pkt.(utils.AVPacket)
	if utils.AVMediaTypeAudio == packet.MediaType() {
		p.audioMemoryPool.FreeHead()
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		p.videoMemoryPool.FreeHead()
	}
}

func (p *Publisher) OnDeMuxStream(stream_ utils.AVStream) {
	//AVStream的Data单独拷贝出来
	//释放掉内存池中最新分配的内存
	tmp := stream_.Extra()
	bytes := make([]byte, len(tmp))
	copy(bytes, tmp)
	stream_.SetExtraData(bytes)

	if utils.AVMediaTypeAudio == stream_.Type() {
		p.audioMemoryPool.FreeTail()
	} else if utils.AVMediaTypeVideo == stream_.Type() {
		p.videoMemoryPool.FreeTail()
	}

	if ret, buffer := p.SourceImpl.OnDeMuxStream(stream_); ret && buffer != nil {
		buffer.SetDiscardHandler(p.OnDiscardPacket)
	}

}

func (p *Publisher) OnDeMuxStreamDone() {

}

func (p *Publisher) OnDeMuxPacket(index int, packet utils.AVPacket) {
	p.SourceImpl.OnDeMuxPacket(index, packet)

	if stream.AppConfig.GOPCache > 0 {
		return
	}

	if utils.AVMediaTypeAudio == packet.MediaType() {
		p.audioMemoryPool.FreeHead()
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		p.videoMemoryPool.FreeHead()
	}
}

func (p *Publisher) OnDeMuxDone() {

}

// OnVideo 从rtm chunk解析过来的视频包
func (p *Publisher) OnVideo(data []byte, ts uint32) {
	if data == nil {
		data = p.videoMemoryPool.Fetch()
		p.videoUnmark = false
	}

	//_ = p.SourceImpl.TransDeMuxer.(libflv.DeMuxer).InputVideo(data, ts)
}

func (p *Publisher) OnAudio(data []byte, ts uint32) {
	if data == nil {
		data = p.audioMemoryPool.Fetch()
		p.audioUnmark = false
	}

	//_ = p.SourceImpl.TransDeMuxer.(libflv.DeMuxer).InputAudio(data, ts)
}

// OnPartPacket 从rtmp解析过来的部分音视频包
func (p *Publisher) OnPartPacket(index int, data []byte, first bool) {
	//audio
	if index == 0 {
		if !p.audioUnmark {
			p.audioMemoryPool.Mark()
			p.audioUnmark = true
		}

		p.audioMemoryPool.Write(data)
		//video
	} else if index == 1 {
		if !p.videoUnmark {
			p.videoMemoryPool.Mark()
			p.videoUnmark = true
		}

		p.videoMemoryPool.Write(data)
	}
}
