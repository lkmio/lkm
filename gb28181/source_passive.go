package gb28181

import (
	"encoding/hex"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/transport"
	"net"
)

type PassiveSource struct {
	stream.StreamServer[GBSource]
	BaseGBSource
	decoder       *transport.LengthFieldFrameDecoder
	receiveBuffer []byte
	remoteAddr    string
}

func (p *PassiveSource) SetupType() SetupType {
	return SetupPassive
}

func (p *PassiveSource) Close() {
	p.BaseGBSource.Close()
	stream.TCPReceiveBufferPool.Put(p.receiveBuffer[:cap(p.receiveBuffer)])
}

func (p *PassiveSource) DecodeGBRTPOverTCPPacket(data []byte) error {
	length := len(data)
	for i := 0; i < length; {
		// 解析粘包数据
		n, bytes, err := p.decoder.Input(data[i:])
		if err != nil {
			return err
		}

		i += n
		if bytes == nil {
			break
		}

		if err = p.ProcessPacket(bytes); err != nil {
			return err
		}
	}

	return nil
}

func (p *PassiveSource) OnConnected(conn net.Conn) []byte {
	p.StreamServer.OnConnected(conn)

	var ok bool
	p.ExecuteWithDeleteLock(func() {
		if p.IsClosed() {
			log.Sugar.Infof("source %s 已关闭, 拒绝新连接", p.GetID())
		} else if ok = p.PublishSource.Conn == nil; ok {
			// 一个推流一个端口, 默认第一个连接为有效连接, 关闭其他连接
			p.PublishSource.Conn = conn
			p.remoteAddr = conn.RemoteAddr().String()
		} else {
			log.Sugar.Infof("port %d 已连接, 关闭连接. source: %s", p.transport.ListenPort(), p.GetID())
		}
	})

	if !ok {
		_ = conn.Close()
		return nil
	}

	return p.receiveBuffer
}

func (p *PassiveSource) OnPacket(conn net.Conn, data []byte) []byte {
	p.StreamServer.OnPacket(conn, data)

	err := p.DecodeGBRTPOverTCPPacket(data)
	if err != nil {
		log.Sugar.Errorf("解析rtp失败 err: %s conn: %s data: %s", err.Error(), conn.RemoteAddr().String(), hex.EncodeToString(data))
		_ = conn.Close()
		return nil
	}

	return p.receiveBuffer
}

func (p *PassiveSource) OnDisConnected(conn net.Conn, err error) {
	p.StreamServer.OnDisConnected(conn, err)

	if conn.RemoteAddr().String() == p.remoteAddr {
		p.Close()
	}
}

func NewPassiveSource() *PassiveSource {
	source := &PassiveSource{
		StreamServer: stream.StreamServer[GBSource]{
			SourceType: stream.SourceType28181,
		},
		decoder:       transport.NewLengthFieldFrameDecoder(0xFFFF, 2),
		receiveBuffer: stream.TCPReceiveBufferPool.Get().([]byte),
	}

	return source
}
