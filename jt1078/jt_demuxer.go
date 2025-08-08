package jt1078

import (
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/utils"
)

type Demuxer struct {
	avformat.BaseDemuxer
	prevPacket *Packet
	sim        string
	channel    int
	lastError  string
	version    int
}

func (d *Demuxer) ProcessPrevPacket() error {
	var codec utils.AVCodecID
	index := d.FindBufferIndex(int(d.prevPacket.pt))
	bytes, err := d.BaseDemuxer.DataPipeline.Fetch(index)
	if err != nil {
		return err
	} else /*if d.prevPacket.packetType > AudioFrameMark {
		// 透传数据, 丢弃
		d.DataPipeline.DiscardBackPacket(index)
		return nil
	} else*/if err, codec = PT2CodecID(d.prevPacket.pt); err != nil {
		d.BaseDemuxer.DataPipeline.DiscardBackPacket(index)
		return err
	}

	if d.prevPacket.packetType == AudioFrameMark {
		d.OnAudioPacket(index, codec, bytes, int64(d.prevPacket.ts))
	} else if d.prevPacket.packetType < AudioFrameMark {
		// 视频帧
		d.OnVideoPacket(index, codec, bytes, avformat.IsKeyFrame(codec, bytes), int64(d.prevPacket.ts), int64(d.prevPacket.ts), avformat.PacketTypeAnnexB)
	}

	if !d.Completed && d.Tracks.Size() > 1 {
		d.TryCompleteProbe()
	}
	return nil
}

func (d *Demuxer) Input(data []byte) (int, error) {
	packet := Packet{
		version: d.version,
	}
	if err := packet.Unmarshal(data); err != nil {
		return 0, err
	} else if len(packet.payload) == 0 {
		// 过滤空数据
		return 0, nil
	}

	// 如果时间戳或者负载类型发生变化, 认为是新的音视频帧，处理前一包，创建AVPacket，回调给PublishSource。
	// 分包标记可能不靠谱
	if d.prevPacket != nil && (d.prevPacket.ts != packet.ts || d.prevPacket.pt != packet.pt) {
		err := d.ProcessPrevPacket()
		if err != nil && err.Error() != d.lastError {
			println(err.Error())
			d.lastError = err.Error()
		}
	}

	if d.prevPacket == nil {
		d.prevPacket = &Packet{}
		d.sim = packet.simNumber
		d.channel = int(packet.channelNumber)
	}

	var mediaType utils.AVMediaType
	if packet.packetType < AudioFrameMark {
		mediaType = utils.AVMediaTypeVideo
	} else if packet.packetType == AudioFrameMark {
		mediaType = utils.AVMediaTypeAudio
	} else {
		// 透传数据, 丢弃
		return len(data), nil
	}

	index := d.FindBufferIndex(int(packet.pt))
	_, err := d.DataPipeline.Write(packet.payload, index, mediaType)
	if err != nil {
		panic(err)
	}

	*d.prevPacket = packet
	return len(data), nil
}

func NewDemuxer(version int) *Demuxer {
	return &Demuxer{
		BaseDemuxer: avformat.BaseDemuxer{
			DataPipeline: &avformat.StreamsBuffer{},
			Name:         "jt1078", // vob
			AutoFree:     false,
		},
		version: version,
	}
}
