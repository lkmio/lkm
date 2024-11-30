package rtsp

import (
	"github.com/lkmio/avformat/librtp"
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/pion/rtcp"
	"net"
	"time"
)

var (
	TransportManger transport.Manager
)

// Sink rtsp拉流sink
// 对于udp而言, 每个sink维护多个transport
// tcp使用信令链路传输
type Sink struct {
	stream.BaseSink

	senders []*librtp.RtpSender // 一个rtsp源, 可能存在多个流, 每个流都需要拉取
	sdpCb   func(sdp string)    // sdp回调, 响应describe
}

func (s *Sink) StartStreaming(transStream stream.TransStream) error {
	if s.senders == nil {
		s.senders = make([]*librtp.RtpSender, transStream.TrackCount())
	}

	// sdp回调给sink, sink应答给describe请求
	if s.sdpCb != nil {
		s.sdpCb(transStream.(*TransStream).sdp)
		s.sdpCb = nil
	}

	return nil
}

func (s *Sink) AddSender(index int, tcp bool, ssrc uint32) (uint16, uint16, error) {
	utils.Assert(index < cap(s.senders))
	utils.Assert(s.senders[index] == nil)

	var err error
	var rtpPort uint16
	var rtcpPort uint16

	sender := librtp.RtpSender{
		SSRC: ssrc,
	}

	if tcp {
		s.TCPStreaming = true
	} else {
		sender.Rtp, err = TransportManger.NewUDPServer("0.0.0.0")
		if err != nil {
			return 0, 0, err
		}

		sender.Rtcp, err = TransportManger.NewUDPServer("0.0.0.0")
		if err != nil {
			sender.Rtp.Close()
			sender.Rtp = nil
			return 0, 0, err
		}

		sender.Rtp.SetHandler2(nil, sender.OnRTPPacket, nil)
		sender.Rtcp.SetHandler2(nil, sender.OnRTCPPacket, nil)
		sender.Rtp.(*transport.UDPServer).Receive()
		sender.Rtcp.(*transport.UDPServer).Receive()

		rtpPort = uint16(sender.Rtp.ListenPort())
		rtcpPort = uint16(sender.Rtcp.ListenPort())
	}

	s.senders[index] = &sender
	return rtpPort, rtcpPort, err
}

func (s *Sink) Write(index int, data [][]byte, rtpTime int64) error {
	// 拉流方还没有连接上来
	if index >= cap(s.senders) || s.senders[index] == nil {
		return nil
	}

	for _, bytes := range data {
		sender := s.senders[index]
		sender.PktCount++
		sender.OctetCount += len(bytes)
		if s.TCPStreaming {
			s.Conn.Write(bytes)
		} else {
			//发送rtcp sr包
			sender.RtpConn.Write(bytes[OverTcpHeaderSize:])

			if sender.RtcpConn == nil || sender.PktCount%100 != 0 {
				continue
			}

			nano := uint64(time.Now().UnixNano())
			ntp := (nano/1000000000 + 2208988800<<32) | (nano % 1000000000)
			sr := rtcp.SenderReport{
				SSRC:        sender.SSRC,
				NTPTime:     ntp,
				RTPTime:     uint32(rtpTime),
				PacketCount: uint32(sender.PktCount),
				OctetCount:  uint32(sender.OctetCount),
			}

			marshal, err := sr.Marshal()
			if err != nil {
				log.Sugar.Errorf("创建rtcp sr消息失败 err:%s msg:%v", err.Error(), sr)
			}

			sender.RtcpConn.Write(marshal)
		}
	}

	return nil
}

// 拉流链路是否已经连接上
// 拉流测发送了play请求, 并且对于udp而言, 还需要收到nat穿透包
func (s *Sink) isConnected(index int) bool {
	return s.TCPStreaming || (s.senders[index] != nil && s.senders[index].RtpConn != nil)
}

func (s *Sink) Close() {
	s.BaseSink.Close()

	for _, sender := range s.senders {
		if sender == nil {
			continue
		}

		if sender.Rtp != nil {
			sender.Rtp.Close()
		}

		if sender.Rtcp != nil {
			sender.Rtcp.Close()
		}
	}
}

func NewSink(id stream.SinkID, sourceId string, conn net.Conn, cb func(sdp string)) stream.Sink {
	return &Sink{
		stream.BaseSink{ID: id, SourceID: sourceId, Protocol: stream.TransStreamRtsp, Conn: conn},
		nil,
		cb,
	}
}
