package rtsp

import (
	"github.com/yangjiechina/avformat/transport"
	"net"
)

type rtspTrack struct {
	rtp  transport.ITransport
	rtcp transport.ITransport

	rtpConn  net.Conn
	rtcpConn net.Conn

	//rtcp
	pktCount   int
	ssrc       uint32
	octetCount int
}

func (s *rtspTrack) onRTPPacket(conn net.Conn, data []byte) {
	if s.rtpConn == nil {
		s.rtpConn = conn
	}
}

func (s *rtspTrack) onRTCPPacket(conn net.Conn, data []byte) {
	if s.rtcpConn == nil {
		s.rtcpConn = conn
	}

	//packs, err := rtcp.Unmarshal(data)
	//if err != nil {
	//	log.Sugar.Warnf("解析rtcp包失败 err:%s conn:%s pkt:%s", err.Error(), conn.RemoteAddr().String(), hex.EncodeToString(data))
	//	return
	//}
	//
	//for _, pkt := range packs {
	//	if _, ok := pkt.(*rtcp.ReceiverReport); ok {
	//	} else if _, ok := pkt.(*rtcp.SourceDescription); ok {
	//	} else if _, ok := pkt.(*rtcp.Goodbye); ok {
	//	}
	//}
}

// tcp链接成功回调
func (s *rtspTrack) onTCPConnected(conn net.Conn) {
	if s.rtcpConn != nil {
		s.rtcpConn = conn
	}
}

// tcp断开链接回调
func (s *rtspTrack) onTCPDisconnected(conn net.Conn, err error) {

}
