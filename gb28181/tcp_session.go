package gb28181

import (
	"fmt"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/transport"
	"github.com/pion/rtp"
	"net"
)

// TCPSession 国标TCP主被动推流Session, 统一处理TCP粘包.
type TCPSession struct {
	conn    net.Conn
	source  GBSource
	decoder *transport.LengthFieldFrameDecoder
}

func (t *TCPSession) Init(source GBSource) {
	t.source = source
}

func (t *TCPSession) Close() {
	t.conn = nil
	if t.source != nil {
		t.source.Close()
		t.source = nil
	}
}

func DecodeGBRTPOverTCPPacket(data []byte, source GBSource, decoder *transport.LengthFieldFrameDecoder, filter Filter, conn net.Conn) (GBSource, error) {
	length := len(data)
	for i := 0; i < length; {
		n, bytes, err := decoder.Input(data[i:])
		if err != nil {
			return source, err
		}

		i += n

		// 单端口模式,ssrc匹配source
		if source == nil || stream.SessionStateHandshakeSuccess == source.State() {
			packet := rtp.Packet{}
			if err := packet.Unmarshal(bytes); err != nil {
				return nil, err
			} else if source == nil {
				source = filter.FindSource(packet.SSRC)
			}

			if source == nil {
				// ssrc 匹配不到Source
				return nil, fmt.Errorf("gb28181推流失败 ssrc: %x 匹配不到source", packet.SSRC)
			}

			if stream.SessionStateHandshakeSuccess == source.State() {
				source.PreparePublish(conn, packet.SSRC, source)
			}
		}

		// 如果是单端口推流, 并且刚才与source绑定, 此时正位于网络收流协程, 否则都位于主协程
		if source.SetupType() == SetupPassive {
			source.(*PassiveSource).BaseGBSource.Input(bytes)
		} else {
			source.(*ActiveSource).BaseGBSource.Input(bytes)
		}
	}

	return source, nil
}

func NewTCPSession(conn net.Conn, filter Filter) *TCPSession {
	session := &TCPSession{
		conn: conn,
		// filter:  filter,
		decoder: transport.NewLengthFieldFrameDecoder(0xFFFF, 2),
	}

	// 多端口收流, Source已知, 直接初始化Session
	if stream.AppConfig.GB28181.IsMultiPort() {
		session.Init(filter.(*singleFilter).source)
	}

	return session
}
