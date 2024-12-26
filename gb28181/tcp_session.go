package gb28181

import (
	"encoding/hex"
	"github.com/lkmio/avformat/transport"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/pion/rtp"
	"net"
)

// TCPSession 国标TCP主被动推流Session, 统一处理TCP粘包.
type TCPSession struct {
	conn          net.Conn
	source        GBSource
	decoder       *transport.LengthFieldFrameDecoder
	receiveBuffer *stream.ReceiveBuffer
}

func (t *TCPSession) Init(source GBSource) {
	t.source = source
	// 创建收流缓冲区
	t.receiveBuffer = stream.NewTCPReceiveBuffer()
	// session关联到source后, 后续流数据都由source处理
	if source.SetupType() == SetupPassive {
		source.(*PassiveSource).decoder = t.decoder
	} else {
		source.(*ActiveSource).decoder = t.decoder
	}
}

func (t *TCPSession) Close() {
	t.conn = nil
	if t.source != nil {
		t.source.Close()
		t.source = nil
	}

	if t.decoder != nil {
		t.decoder.Close()
		t.decoder = nil
	}
}

func NewTCPSession(conn net.Conn, filter Filter) *TCPSession {
	session := &TCPSession{
		conn: conn,
	}

	// 创建粘包解码器, 并设置解粘包处理回调
	session.decoder = transport.NewLengthFieldFrameDecoder(0xFFFF, 2, func(bytes []byte) {
		// 单端口模式,ssrc匹配source
		if session.source == nil || stream.SessionStateHandshakeSuccess == session.source.State() {
			packet := rtp.Packet{}
			if err := packet.Unmarshal(bytes); err != nil {
				log.Sugar.Errorf("解析rtp失败 err: %s conn: %s data: %s", err.Error(), conn.RemoteAddr().String(), hex.EncodeToString(bytes))
				conn.Close()
				return
			}

			source := filter.FindSource(packet.SSRC)
			if source == nil {
				// 匹配不到Source, 直接关闭连接
				log.Sugar.Errorf("gb28181推流失败 ssrc: %x 匹配不到source conn: %s  data: %s", packet.SSRC, session.conn.RemoteAddr().String(), hex.EncodeToString(bytes))
				conn.Close()
				return
			}

			session.Init(source)

			if stream.SessionStateHandshakeSuccess == session.source.State() {
				session.source.PreparePublish(session.conn, packet.SSRC, session.source)
			}
		}

		// 如果是单端口推流, 并且刚才与source绑定, 此时正位于网络收流协程, 否则都位于主协程
		if session.source.SetupType() == SetupPassive {
			session.source.(*PassiveSource).BaseGBSource.Input(bytes)
		} else {
			session.source.(*ActiveSource).BaseGBSource.Input(bytes)
		}
	})

	// 多端口收流, Source已知, 直接初始化Session
	if stream.AppConfig.GB28181.IsMultiPort() {
		session.Init(filter.(*singleFilter).source)
	}

	return session
}
