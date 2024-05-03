package rtsp

import (
	"fmt"
	"github.com/pion/rtcp"
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/log"
	"github.com/yangjiechina/live-server/stream"
	"net"
	"time"
)

var (
	TransportManger stream.TransportManager
)

// 对于UDP而言, 每个sink维护一对UDPTransport
// TCP直接单端口传输
type sink struct {
	stream.SinkImpl

	//一个rtsp源，可能存在多个流, 每个流都需要拉取拉取
	tracks []*rtspTrack
	sdpCb  func(sdp string)

	//是否是TCP拉流
	tcp     bool
	playing bool
}

func NewSink(id stream.SinkId, sourceId string, conn net.Conn, cb func(sdp string)) stream.ISink {
	return &sink{
		stream.SinkImpl{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolRtsp, Conn: conn},
		nil,
		cb,
		false,
		false,
	}
}

func (s *sink) setTrackCount(count int) {
	s.tracks = make([]*rtspTrack, count)
}

func (s *sink) addTrack(index int, tcp bool, ssrc uint32) (uint16, uint16, error) {
	utils.Assert(index < cap(s.tracks))
	utils.Assert(s.tracks[index] == nil)

	var err error
	var rtpPort uint16
	var rtcpPort uint16

	track := rtspTrack{
		ssrc: ssrc,
	}
	if tcp {
		s.tcp = true
	} else {
		err = TransportManger.AllocPairTransport(func(port uint16) error {
			//rtp port
			var addr *net.UDPAddr
			addr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", "0.0.0.0", port))
			if err == nil {
				track.rtp = &transport.UDPTransport{}
				track.rtp.SetHandler2(nil, track.onRTPPacket, nil)
				err = track.rtp.Bind(addr)
			}

			rtpPort = port
			return nil
		}, func(port uint16) error {
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

			return nil
		})
	}

	if err != nil {
		return 0, 0, err
	}

	s.tracks[index] = &track
	return rtpPort, rtcpPort, err
}

func (s *sink) input(index int, data []byte, rtpTime uint32) error {
	//拉流方还没有连上来
	utils.Assert(index < cap(s.tracks))

	track := s.tracks[index]
	track.pktCount++
	track.octetCount += len(data)
	if s.tcp {
		s.Conn.Write(data)
	} else {
		track.rtpConn.Write(data)

		if track.rtcpConn == nil || track.pktCount%100 != 0 {
			return nil
		}

		nano := uint64(time.Now().UnixNano())
		ntp := (nano/1000000000 + 2208988800<<32) | (nano % 1000000000)
		sr := rtcp.SenderReport{
			SSRC:        track.ssrc,
			NTPTime:     ntp,
			RTPTime:     rtpTime,
			PacketCount: uint32(track.pktCount),
			OctetCount:  uint32(track.octetCount),
		}

		marshal, err := sr.Marshal()
		if err != nil {
			log.Sugar.Errorf("创建rtcp sr消息失败 err:%s msg:%v", err.Error(), sr)
		}

		track.rtcpConn.Write(marshal)
	}
	return nil
}

func (s *sink) isConnected(index int) bool {
	return s.playing && (s.tcp || (s.tracks[index] != nil && s.tracks[index].rtpConn != nil))
}

func (s *sink) pktCount(index int) int {
	return s.tracks[index].pktCount
}

// SendHeader 回调rtsp流的sdp信息
func (s *sink) SendHeader(data []byte) error {
	s.sdpCb(string(data))
	return nil
}

func (s *sink) TrackConnected(index int) bool {
	utils.Assert(index < cap(s.tracks))
	utils.Assert(s.tracks[index].rtp != nil)

	return s.tracks[index].rtcpConn != nil
}

func (s *sink) Close() {
	s.SinkImpl.Close()

	for _, track := range s.tracks {
		if track.rtp != nil {
			track.rtp.Close()
		}

		if track.rtcp != nil {
			track.rtcp.Close()
		}
	}
}
