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
	conn          net.Conn
	source        GBSource
	decoder       *transport.LengthFieldFrameDecoder
	receiveBuffer []byte
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

	stream.TCPReceiveBufferPool.Put(t.receiveBuffer[:cap(t.receiveBuffer)])
}

func (t *TCPSession) DecodeGBRTPOverTCPPacket(data []byte, filter Filter, conn net.Conn) error {
	length := len(data)
	for i := 0; i < length; {
		// 解析粘包数据
		n, bytes, err := t.decoder.Input(data[i:])
		if err != nil {
			return err
		}

		i += n
		if bytes == nil {
			break
		}

		// 单端口模式,ssrc匹配source
		if t.source == nil || stream.SessionStateHandshakeSuccess == t.source.State() {
			packet := rtp.Packet{}
			if err = packet.Unmarshal(bytes); err != nil {
				return err
			} else if t.source == nil {
				t.source = filter.FindSource(packet.SSRC)
			}

			if t.source == nil {
				// ssrc 匹配不到Source
				return fmt.Errorf("gb28181推流失败 ssrc: %x 匹配不到source", packet.SSRC)
			}

			if stream.SessionStateHandshakeSuccess == t.source.State() {
				t.source.PreparePublish(conn, packet.SSRC, t.source)
			}
		}

		if err = t.source.ProcessPacket(bytes); err != nil {
			return err
		}
	}

	return nil
}

func NewTCPSession(conn net.Conn, filter Filter) *TCPSession {
	session := &TCPSession{
		conn: conn,
		// filter:  filter,
		decoder:       transport.NewLengthFieldFrameDecoder(0xFFFF, 2),
		receiveBuffer: stream.TCPReceiveBufferPool.Get().([]byte),
	}

	// 多端口收流, Source已知, 直接初始化Session
	if stream.AppConfig.GB28181.IsMultiPort() {
		session.Init(filter.(*singleFilter).source)
	}

	return session
}
