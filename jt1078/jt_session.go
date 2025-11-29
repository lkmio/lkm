package jt1078

import (
	"fmt"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/transport"
	"net"
	"strconv"
	"time"
)

type Session struct {
	stream.PublishSource
	decoder       *transport.DelimiterFrameDecoder
	receiveBuffer []byte
}

func (s *Session) Input(data []byte) (int, error) {
	var n int
	for length := len(data); n < length; {
		i, bytes, err := s.decoder.Input(data[n:])
		if err != nil {
			return -1, err
		} else if len(bytes) < 1 {
			break
		}

		n += i
		demuxer := s.TransDemuxer.(*Demuxer)
		firstOfPacket := demuxer.prevPacket == nil
		_, err = s.PublishSource.Input(bytes)
		if err != nil {
			return -1, err
		}

		// 首包处理
		if firstOfPacket && demuxer.prevPacket != nil {
			s.SetID(demuxer.sim + "/" + strconv.Itoa(demuxer.channel))
			stream.PreparePublishSourceWithAsync(s, true)
		}
	}

	return 0, nil
}

func (s *Session) Close() {
	log.Sugar.Infof("1078推流结束 %s", s.String())

	s.PublishSource.Close()
}

func NewSession(conn net.Conn, version int) *Session {
	delimiter := [4]byte{0x30, 0x31, 0x63, 0x64}
	session := Session{
		PublishSource: stream.PublishSource{
			Conn:         conn,
			Type:         stream.SourceType1078,
			TransDemuxer: NewDemuxer(version),
			SessionID:    fmt.Sprintf("%d", time.Now().UnixMilli()),
		},

		decoder:       transport.NewDelimiterFrameDecoder(1024*1024*2, delimiter[:]),
		receiveBuffer: stream.TCPReceiveBufferPool.Get().([]byte),
	}

	session.TransDemuxer.SetHandler(&session)
	session.TransDemuxer.SetOnPreprocessPacketHandler(session.PublishSource.OnPreprocessPacket)
	session.Init()
	stream.LoopEvent(&session)
	return &session
}
