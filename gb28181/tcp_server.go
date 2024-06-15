package gb28181

import (
	"github.com/pion/rtp"
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/lkm/log"
	"github.com/yangjiechina/lkm/stream"
	"net"
)

type TCPServer struct {
	tcp    *transport.TCPServer
	filter Filter
}

type TCPSession struct {
	source        GBSource
	decoder       *transport.LengthFieldFrameDecoder
	receiveBuffer *stream.ReceiveBuffer
}

func NewTCPServer(addr net.Addr, filter Filter) (*TCPServer, error) {
	server := &TCPServer{
		filter: filter,
	}

	tcp := &transport.TCPServer{}
	tcp.SetHandler(server)
	if err := tcp.Bind(addr); err != nil {
		return server, err
	}

	server.tcp = tcp
	return server, nil
}

func (T *TCPServer) OnConnected(conn net.Conn) []byte {
	log.Sugar.Infof("GB28181连接 conn:%s", conn.RemoteAddr().String())

	con := conn.(*transport.Conn)
	session := &TCPSession{}
	if stream.AppConfig.GB28181.IsMultiPort() {
		session.source = T.filter.(*singleFilter).source
		session.source.SetConn(con)
		session.receiveBuffer = stream.NewTCPReceiveBuffer()
	}

	session.decoder = transport.NewLengthFieldFrameDecoder(0xFFFF, 2, func(bytes []byte) {
		packet := rtp.Packet{}
		err := packet.Unmarshal(bytes)
		if err != nil {
			log.Sugar.Errorf("解析rtp失败 err:%s conn:%s", err.Error(), conn.RemoteAddr().String())
			return
		}

		//单端口模式,ssrc匹配source
		if session.source == nil {
			//匹配不到直接关闭链接
			source := T.filter.FindSource(packet.SSRC)
			if source == nil {
				conn.Close()
				return
			}

			session.source = source
			session.receiveBuffer = stream.NewTCPReceiveBuffer()
			session.source.SetConn(con)

			//直接丢给ps解析器, 虽然是非线程安全, 但是是阻塞执行的, 不会和后续走loop event的包冲突
			session.source.InputRtp(&packet)
		}

		if stream.SessionStateHandshakeDone == session.source.State() {
			session.source.PreparePublishSource(conn, packet.SSRC, session.source)
		}

		session.source.InputRtp(&packet)
	})

	con.Data = session

	if session.source != nil {
		return session.receiveBuffer.GetBlock()
	}

	return nil
}

func (T *TCPServer) OnPacket(conn net.Conn, data []byte) []byte {
	session := conn.(*transport.Conn).Data.(*TCPSession)

	//单端口收流
	if session.source == nil {
		//直接传给解码器, 先根据ssrc找到source. 后续还是会直接传给source
		if err := session.decoder.Input(data); err != nil {
			conn.Close()
		}
	} else {
		session.source.Input(data)
	}

	return session.receiveBuffer.GetBlock()
}

func (T *TCPServer) OnDisConnected(conn net.Conn, err error) {
	log.Sugar.Infof("GB28181断开连接 conn:%s", conn.RemoteAddr().String())

	con := conn.(*transport.Conn)
	if con.Data != nil && con.Data.(*TCPSession).source != nil {
		con.Data.(*TCPSession).source.Close()
	}
	con.Data = nil
}
