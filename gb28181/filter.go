package gb28181

import (
	"github.com/pion/rtp"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/log"
	"github.com/yangjiechina/lkm/stream"
	"net"
)

type Filter interface {
	AddSource(ssrc uint32, source GBSource) bool

	RemoveSource(ssrc uint32)

	Input(conn net.Conn, data []byte) GBSource

	ParseRtpPacket(conn net.Conn, data []byte) (*rtp.Packet, error)

	PreparePublishSource(conn net.Conn, ssrc uint32, source GBSource)
}

type BaseFilter struct {
}

func (r BaseFilter) ParseRtpPacket(conn net.Conn, data []byte) (*rtp.Packet, error) {
	packet := rtp.Packet{}
	err := packet.Unmarshal(data)

	if err != nil {
		log.Sugar.Errorf("解析rtp失败 err:%s conn:%s", err.Error(), conn.RemoteAddr().String())
		return nil, err
	}

	return &packet, err
}

func (r BaseFilter) PreparePublishSource(conn net.Conn, ssrc uint32, source GBSource) {
	source.SetConn(conn)
	source.SetSSRC(ssrc)

	source.SetState(stream.SessionStateTransferring)

	if stream.AppConfig.Hook.EnablePublishEvent() {
		go func() {
			_, state := stream.HookPublishEvent(source)
			if utils.HookStateOK != state {
				log.Sugar.Errorf("GB28181 推流失败")

				if conn != nil {
					conn.Close()
				}
			}
		}()
	}
}
