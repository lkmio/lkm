package rtsp

import (
	"fmt"
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
	"net"
)

// 对于UDP而言, 每个sink维护一对UDPTransport
// TCP直接单端口传输
type sink struct {
	stream.SinkImpl

	//一个rtsp源，可能存在多个流, 每个流都需要拉取拉取
	tracks []*rtspTrack
	sdpCB  func(sdp string)
}

func NewSink(id stream.SinkId, sourceId string, conn net.Conn, cb func(sdp string)) stream.ISink {
	return &sink{
		stream.SinkImpl{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolRtsp, Conn: conn},
		nil,
		cb,
	}
}

func (s *sink) setTrackCount(count int) {
	s.tracks = make([]*rtspTrack, count)
}

func (s *sink) addTrack(index int, tcp bool) (int, int, error) {
	utils.Assert(index < cap(s.tracks))
	utils.Assert(s.tracks[index] == nil)

	var err error
	var rtpPort int
	var rtcpPort int

	track := rtspTrack{}
	if tcp {
		err = rtspTransportManger.AllocTransport(true, func(port int) {
			var addr *net.TCPAddr
			addr, err = net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", "0.0.0.0", port))
			if err == nil {
				track.rtp = &transport.TCPServer{}
				track.rtp.SetHandler2(track.onTCPConnected, nil, track.onTCPDisconnected)
				err = track.rtp.Bind(addr)
			}

			rtpPort = port
		})

	} else {
		err = rtspTransportManger.AllocPairTransport(func(port int) {
			//rtp port
			var addr *net.UDPAddr
			addr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", "0.0.0.0", port))
			if err == nil {
				track.rtp = &transport.UDPTransport{}
				track.rtp.SetHandler2(nil, track.onRTPPacket, nil)
				err = track.rtp.Bind(addr)
			}

			rtpPort = port
		}, func(port int) {
			//rtcp port
			var addr *net.UDPAddr
			addr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", "0.0.0.0", port))
			if err == nil {
				track.rtcp = &transport.UDPTransport{}
				track.rtcp.SetHandler2(nil, track.onRTCPPacket, nil)
				err = track.rtcp.Bind(addr)
			} else {
				track.rtp.Close()
				track.rtp = nil
			}

			rtcpPort = port
		})
	}

	if err != nil {
		return 0, 0, err
	}

	s.tracks[index] = &track
	return rtpPort, rtcpPort, err
}

func (s *sink) input(index int, data []byte) error {
	utils.Assert(index < cap(s.tracks))

	//拉流方还没有连上来

	s.tracks[index].pktCount++
	s.tracks[index].rtpConn.Write(data)
	return nil
}

func (s *sink) isConnected(index int) bool {
	return s.tracks[index] != nil && s.tracks[index].rtpConn != nil
}

func (s *sink) pktCount(index int) int {
	return s.tracks[index].pktCount
}

// SendHeader 回调rtsp流的sdp信息
func (s *sink) SendHeader(data []byte) error {
	s.sdpCB(string(data))
	return nil
}

func (s *sink) TrackConnected(index int) bool {
	utils.Assert(index < cap(s.tracks))
	utils.Assert(s.tracks[index].rtp != nil)

	return s.tracks[index].rtcpConn != nil
}

func (s *sink) Close() {
	for _, track := range s.tracks {
		if track.rtp != nil {
			track.rtp.Close()
		}

		if track.rtcp != nil {
			track.rtcp.Close()
		}
	}
}
