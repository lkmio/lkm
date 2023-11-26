package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

type Publisher struct {
	stream.SourceImpl
	deMuxer         libflv.DeMuxer
	audioMemoryPool stream.MemoryPool
	videoMemoryPool stream.MemoryPool

	audioUnmark bool
	videoUnmark bool
}

func NewPublisher(sourceId string) *Publisher {
	publisher := &Publisher{SourceImpl: stream.SourceImpl{Id_: sourceId}, audioUnmark: false, videoUnmark: false}
	publisher.deMuxer = libflv.DeMuxer{}
	//设置回调，从flv解析出来的Stream和AVPacket都将统一回调到stream.SourceImpl
	publisher.deMuxer.SetHandler(publisher)

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

	p.SourceImpl.OnDeMuxStream(stream_)
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

	_ = p.deMuxer.InputVideo(data, ts)
}

func (p *Publisher) OnAudio(data []byte, ts uint32) {
	if data == nil {
		data = p.audioMemoryPool.Fetch()
		p.audioUnmark = false
	}

	_ = p.deMuxer.InputAudio(data, ts)
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
