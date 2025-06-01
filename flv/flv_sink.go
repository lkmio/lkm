package flv

import (
	"encoding/binary"
	"github.com/lkmio/avformat/collections"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/transport"
	"net"
)

type Sink struct {
	stream.BaseSink
	prevTagSize uint32
}

func (s *Sink) StopStreaming(stream stream.TransStream) {
	s.BaseSink.StopStreaming(stream)
	s.prevTagSize = stream.(*TransStream).Muxer.PrevTagSize()
}

func (s *Sink) Write(index int, data []*collections.ReferenceCounter[[]byte], ts int64, keyVideo bool) error {
	var offset int
	if s.SentPacketCount == 0 {
		// 恢复推流时, 不发送9个字节的flv header
		if s.prevTagSize > 0 {
			if s.prevTagSize > 0 {
				data = data[1:]
			}
		} else {
			offset++
		}

	}

	var modifiedTags []*collections.ReferenceCounter[[]byte]
	// 修改第一个tag的prevTagSize, 如果第一个tag的prevTagSize与sink的prevTagSize不一致
	if s.SentPacketCount < 2 {
		tags := SplitHttpFlvBlock(data[offset].Get())
		if s.prevTagSize != tags[0].PrevTagSize {
			for _, datum := range data {
				modifiedTags = append(modifiedTags, datum)
			}

			bytes := make([]byte, len(data[offset].Get()))
			copy(bytes, data[offset].Get())
			binary.BigEndian.PutUint32(bytes[tags[0].Offset:], s.prevTagSize)
			modifiedTags[offset] = collections.NewReferenceCounter(bytes)
		}

		s.prevTagSize = uint32(11 + tags[len(tags)-1].DataSize)
	}

	if modifiedTags == nil {
		modifiedTags = data
	}

	return s.BaseSink.Write(index, modifiedTags, ts, keyVideo)
}

func NewFLVSink(id stream.SinkID, sourceId string, conn net.Conn) stream.Sink {
	return &Sink{BaseSink: stream.BaseSink{ID: id, SourceID: sourceId, State: stream.SessionStateCreated, Protocol: stream.TransStreamFlv, Conn: transport.NewConn(conn), TCPStreaming: true}}
}
