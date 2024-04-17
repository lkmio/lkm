package rtmp

import (
	"github.com/yangjiechina/avformat/libflv"
	"github.com/yangjiechina/avformat/librtmp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
	"net"
)

type Publisher interface {

	// Init 初始化内存池
	Init()

	// OnDiscardPacket GOP缓存溢出的包
	OnDiscardPacket(pkt interface{})

	// OnVideo 从rtmp chunk中解析出来的整个视频包, 还需要进步封装成AVPacket
	OnVideo(data []byte, ts uint32)

	// OnAudio 从rtmp chunk中解析出来的整个音频包
	OnAudio(data []byte, ts uint32)

	// OnPartPacket 从rtmp chunk中解析出来的一部分音视频包
	OnPartPacket(index int, data []byte, first bool)
}

type publisher struct {
	stream.SourceImpl

	stack           *librtmp.Stack
	audioMemoryPool stream.MemoryPool
	videoMemoryPool stream.MemoryPool

	audioMark bool
	videoMark bool
}

func NewPublisher(sourceId string, stack *librtmp.Stack, conn net.Conn) Publisher {
	deMuxer := libflv.NewDeMuxer(libflv.TSModeRelative)
	publisher_ := &publisher{SourceImpl: stream.SourceImpl{Id_: sourceId, Type_: stream.SourceTypeRtmp, TransDeMuxer: deMuxer, Conn: conn}, stack: stack, audioMark: false, videoMark: false}
	//设置回调，从flv解析出来的Stream和AVPacket都将统一回调到stream.SourceImpl
	deMuxer.SetHandler(publisher_)
	publisher_.Input_ = publisher_.Input

	return publisher_
}

func (p *publisher) Init() {
	//创建内存池
	p.audioMemoryPool = stream.NewMemoryPool(48000 * 64)
	if stream.AppConfig.GOPCache {
		//以每秒钟4M码率大小创建内存池
		p.videoMemoryPool = stream.NewMemoryPool(4096 * 1000)
	} else {
		p.videoMemoryPool = stream.NewMemoryPool(4096 * 1000 / 8)
	}

	p.SourceImpl.Init()
	go p.SourceImpl.LoopEvent()
}

func (p *publisher) Input(data []byte) {
	p.stack.Input(nil, data)
}

func (p *publisher) OnDiscardPacket(pkt interface{}) {
	packet := pkt.(utils.AVPacket)
	if utils.AVMediaTypeAudio == packet.MediaType() {
		p.audioMemoryPool.FreeHead()
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		p.videoMemoryPool.FreeHead()
	}
}

func (p *publisher) OnDeMuxStream(stream_ utils.AVStream) {
	//释放掉内存池中最新分配的内存
	if utils.AVMediaTypeAudio == stream_.Type() {
		p.audioMemoryPool.FreeTail()
	} else if utils.AVMediaTypeVideo == stream_.Type() {
		p.videoMemoryPool.FreeTail()
	}

	if ret, buffer := p.SourceImpl.OnDeMuxStream(stream_); ret && buffer != nil {
		buffer.SetDiscardHandler(p.OnDiscardPacket)
	}
}

func (p *publisher) OnDeMuxPacket(packet utils.AVPacket) {
	p.SourceImpl.OnDeMuxPacket(packet)

	if stream.AppConfig.GOPCache {
		return
	}

	//未开启GOP缓存，释放掉内存
	if utils.AVMediaTypeAudio == packet.MediaType() {
		p.audioMemoryPool.FreeTail()
	} else if utils.AVMediaTypeVideo == packet.MediaType() {
		p.videoMemoryPool.FreeTail()
	}
}

// OnVideo 解析出来的完整视频包
// @ts 	   rtmp chunk的相对时间戳
func (p *publisher) OnVideo(data []byte, ts uint32) {
	if data == nil {
		data = p.videoMemoryPool.Fetch()
		p.videoMark = false
	}

	p.SourceImpl.TransDeMuxer.(libflv.DeMuxer).InputVideo(data, ts)
}

func (p *publisher) OnAudio(data []byte, ts uint32) {
	if data == nil {
		data = p.audioMemoryPool.Fetch()
		p.audioMark = false
	}

	_ = p.SourceImpl.TransDeMuxer.(libflv.DeMuxer).InputAudio(data, ts)
}

func (p *publisher) OnPartPacket(index int, data []byte, first bool) {
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
