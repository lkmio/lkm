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

// rtsp拉流sink
// 对于udp而言, 每个sink维护多个transport
// tcp直接单端口传输
type sink struct {
	stream.BaseSink

	senders []*librtp.RtpSender //一个rtsp源，可能存在多个流, 每个流都需要拉取
	sdpCb   func(sdp string)    //rtsp_stream生成sdp后，使用该回调给rtsp_session, 响应describe

	tcp     bool //tcp拉流标记
	playing bool //是否已经收到play请求
}

func NewSink(id stream.SinkId, sourceId string, conn net.Conn, cb func(sdp string)) stream.Sink {
	return &sink{
		stream.BaseSink{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolRtsp, Conn: conn},
		nil,
		cb,
		false,
		false,
	}
}

func (s *sink) setSenderCount(count int) {
	s.senders = make([]*librtp.RtpSender, count)
}

func (s *sink) addSender(index int, tcp bool, ssrc uint32) (uint16, uint16, error) {
	utils.Assert(index < cap(s.senders))
	utils.Assert(s.senders[index] == nil)

	var err error
	var rtpPort uint16
	var rtcpPort uint16

	sender := librtp.RtpSender{
		SSRC: ssrc,
	}

	//tcp使用信令链路
	if tcp {
		s.tcp = true
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

func (s *sink) input(index int, data []byte, rtpTime uint32) error {
	//拉流方还没有连上来
	utils.Assert(index < cap(s.senders))

	sender := s.senders[index]
	sender.PktCount++
	sender.OctetCount += len(data)
	if s.tcp {
		s.Conn.Write(data)
	} else {
		//发送rtcp sr包
		sender.RtpConn.Write(data)

		if sender.RtcpConn == nil || sender.PktCount%100 != 0 {
			return nil
		}

		nano := uint64(time.Now().UnixNano())
		ntp := (nano/1000000000 + 2208988800<<32) | (nano % 1000000000)
		sr := rtcp.SenderReport{
			SSRC:        sender.SSRC,
			NTPTime:     ntp,
			RTPTime:     rtpTime,
			PacketCount: uint32(sender.PktCount),
			OctetCount:  uint32(sender.OctetCount),
		}

		marshal, err := sr.Marshal()
		if err != nil {
			log.Sugar.Errorf("创建rtcp sr消息失败 err:%s msg:%v", err.Error(), sr)
		}

		sender.RtcpConn.Write(marshal)
	}

	return nil
}

// 拉流链路是否已经连接上
// 拉流测发送了play请求, 并且对于udp而言, 还需要收到nat穿透包
func (s *sink) isConnected(index int) bool {
	return s.playing && (s.tcp || (s.senders[index] != nil && s.senders[index].RtpConn != nil))
}

// 发送rtp包总数
func (s *sink) pktCount(index int) int {
	return s.senders[index].PktCount
}

// SendHeader 回调rtsp stream的sdp信息
func (s *sink) SendHeader(data []byte) error {
	s.sdpCb(string(data))
	return nil
}

func (s *sink) Close() {
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
