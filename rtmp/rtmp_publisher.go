package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
)

type Publisher struct {
	stream.SourceImpl

	stack           *librtmp.Stack
	audioMemoryPool stream.MemoryPool
	videoMemoryPool stream.MemoryPool

	audioMark bool
	videoMark bool
}

func NewPublisher(sourceId string, stack *librtmp.Stack) *Publisher {
	deMuxer := libflv.NewDeMuxer()
	publisher := &Publisher{SourceImpl: stream.SourceImpl{Id_: sourceId, Type_: stream.SourceTypeRtmp, TransDeMuxer: deMuxer}, stack: stack, audioMark: false, videoMark: false}
	//设置回调，从flv解析出来的Stream和AVPacket都将统一回调到stream.SourceImpl
	deMuxer.SetHandler(publisher)
	publisher.Input_ = publisher.Input

	return publisher
}

func (p *Publisher) Init() {
	//创建内存池
	p.audioMemoryPool = stream.NewMemoryPool(48000 * 1)
	if stream.AppConfig.GOPCache {
		//以每秒钟4M码率大小创建内存池
		p.videoMemoryPool = stream.NewMemoryPool(4096 * 1000)
	} else {
		p.videoMemoryPool = stream.NewMemoryPool(4096 * 1000 / 8)
	}

	p.SourceImpl.Init()
	go p.SourceImpl.LoopEvent()
}

func (p *Publisher) Input(data []byte) {
	p.stack.Input(nil, data)
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

func (p *Publisher) OnDeMuxPacket(packet utils.AVPacket) {
	p.SourceImpl.OnDeMuxPacket(packet)

	if stream.AppConfig.GOPCache {
		return
	}

	if utils.AVMediaTypeAudio == packet.MediaType() {
		p.audioMemoryPool.FreeHead()
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		p.videoMemoryPool.FreeHead()
	}
}

// OnVideo 从rtm chunk解析过来的视频包
func (p *Publisher) OnVideo(data []byte, ts uint32) {
	if data == nil {
		data = p.videoMemoryPool.Fetch()
		p.videoMark = false
	}

	p.SourceImpl.TransDeMuxer.(*libflv.DeMuxer).InputVideo(data, ts)
}

func (p *Publisher) OnAudio(data []byte, ts uint32) {
	if data == nil {
		data = p.audioMemoryPool.Fetch()
		p.audioMark = false
	}

	_ = p.SourceImpl.TransDeMuxer.(*libflv.DeMuxer).InputAudio(data, ts)
}

// OnPartPacket 从rtmp解析过来的部分音视频包
func (p *Publisher) OnPartPacket(index int, data []byte, first bool) {
	//audio
	if index == 0 {
		if !p.audioMark {
			p.audioMemoryPool.Mark()
			p.audioMark = true
		}

		p.audioMemoryPool.Write(data)
		//video
	} else if index == 1 {
		if !p.videoMark {
			p.videoMemoryPool.Mark()
			p.videoMark = true
		}

		p.videoMemoryPool.Write(data)
	}
}
