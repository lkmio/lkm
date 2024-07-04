package gb28181

import (
	"encoding/hex"
	"github.com/pion/rtp"
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/lkm/log"
	"github.com/yangjiechina/lkm/stream"
	"net"
)

type TCPSession struct {
	conn          net.Conn
	source        GBSource
	decoder       *transport.LengthFieldFrameDecoder
	receiveBuffer *stream.ReceiveBuffer
}

// Input 处理source收到的流
func (t *TCPSession) Input(data []byte) error {
	if err := t.decoder.Input(data); err != nil {
		t.conn.Close()
	}

	return nil
}

func (t *TCPSession) Init(source GBSource) {
	t.source = source
	t.source.SetConn(t.conn)
	//重新设置收流回调
	t.source.SetInputCb(t.Input)
	t.receiveBuffer = stream.NewTCPReceiveBuffer()
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

	if stream.AppConfig.GB28181.IsMultiPort() {
		session.Init(filter.(*singleFilter).source)
	}

	session.decoder = transport.NewLengthFieldFrameDecoder(0xFFFF, 2, func(bytes []byte) {
		packet := rtp.Packet{}
		err := packet.Unmarshal(bytes)
		if err != nil {
			log.Sugar.Errorf("解析rtp失败 err:%s conn:%s data:%s", err.Error(), conn.RemoteAddr().String(), hex.EncodeToString(bytes))

			conn.Close()
			return
		}

		//单端口模式,ssrc匹配source
		if session.source == nil {
			//匹配不到直接关闭链接
			source := filter.FindSource(packet.SSRC)
			if source == nil {
				log.Sugar.Errorf("gb28181推流失败 ssrc:%x配置不到source conn:%s  data:%s", packet.SSRC, session.conn.RemoteAddr().String(), hex.EncodeToString(bytes))

				conn.Close()
				return
			}

			session.Init(source)
		}

		if stream.SessionStateHandshakeDone == session.source.State() {
			session.source.PreparePublish(conn, packet.SSRC, session.source)
		}

		session.source.InputRtp(&packet)
	})

	return session
}
