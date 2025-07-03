package stream

import (
	"github.com/lkmio/avformat"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/transcode"
	"sync"
)

var (
	AudioPacketDataPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 48000)
		},
	}
)

type TranscodeTrack struct {
	track      *Track
	packets    *collections.Queue[*avformat.AVPacket]
	transcoder transcode.Transcoder
	result     []*avformat.AVPacket

	dst int64
	pts int64
}

func (t *TranscodeTrack) Input(packet *avformat.AVPacket) []*avformat.AVPacket {
	t.result = t.result[:0]

	_, _ = t.transcoder.Transcode(packet, func(bytes []byte, duration int) {
		dstPkt := avformat.PacketPool.Get().(*avformat.AVPacket)
		*dstPkt = *packet

		data := AudioPacketDataPool.Get().([]byte)
		copy(data, bytes)

		dstPkt.Data = data[:len(bytes)]
		dstPkt.Timebase = 1000
		dstPkt.Pts = t.pts
		dstPkt.Dts = t.pts
		dstPkt.Duration = int64(duration)
		dstPkt.CodecID = t.track.Stream.CodecID
		dstPkt.Index = t.track.Stream.Index

		t.pts += dstPkt.Duration
		t.dst += dstPkt.Duration
		t.packets.Push(dstPkt)
		t.result = append(t.result, dstPkt)
	})

	return t.result
}

func (t *TranscodeTrack) Clear() {
	for t.packets.Size() > 0 {
		packet := t.packets.Pop()
		AudioPacketDataPool.Put(packet.Data[:cap(packet.Data)])
		avformat.FreePacket(packet)
	}
}

func (t *TranscodeTrack) Close() {
	t.transcoder.Close()
	t.Clear()
}

func NewTranscodeTrack(track *Track, transcoder transcode.Transcoder) *TranscodeTrack {
	return &TranscodeTrack{
		track:      track,
		transcoder: transcoder,
		packets:    collections.NewQueue[*avformat.AVPacket](128),
	}
}
