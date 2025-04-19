package jt1078

import (
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/transport"
	"net"
	"strconv"
)

type Session struct {
	stream.PublishSource
	decoder *transport.DelimiterFrameDecoder
}

func (s *Session) Input(data []byte) error {
	var n int
	for length := len(data); n < length; {
		i, bytes, err := s.decoder.Input(data[n:])
		if err != nil {
			return err
		} else if len(bytes) < 1 {
			break
		}

		n += i
		demuxer := s.TransDemuxer.(*Demuxer)
		firstOfPacket := demuxer.prevPacket == nil
		_, err = demuxer.Input(bytes)
		if err != nil {
			return err
		}

		// 首包处理, hook通知
		if firstOfPacket && demuxer.prevPacket != nil {
			s.SetID(demuxer.sim + "/" + strconv.Itoa(demuxer.channel))

			go func() {
				_, state := stream.PreparePublishSource(s, true)
				if utils.HookStateOK != state {
					log.Sugar.Errorf("1078推流失败 source: %s", demuxer.sim)

					if s.Conn != nil {
						s.Conn.Close()
					}
				}
			}()
		}
	}

	return nil
}

func (s *Session) Close() {
	log.Sugar.Infof("1078推流结束 %s", s.String())

	if s.Conn != nil {
		s.Conn.Close()
		s.Conn = nil
	}

	s.PublishSource.Close()
}

func NewSession(conn net.Conn) *Session {
	delimiter := [4]byte{0x30, 0x31, 0x63, 0x64}
	session := Session{
		PublishSource: stream.PublishSource{
			Conn:         conn,
			Type:         stream.SourceType1078,
			TransDemuxer: NewDemuxer(),
		},

		decoder: transport.NewDelimiterFrameDecoder(1024*1024*2, delimiter[:]),
	}

	session.TransDemuxer.SetHandler(&session)
	session.Init(stream.TCPReceiveBufferQueueSize)
	go stream.LoopEvent(&session)
	return &session
}
