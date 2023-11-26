package rtmp

import (
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
	"net"
	"testing"
)

func CreateTransStream(protocol stream.Protocol, streams []utils.AVStream) stream.ITransStream {
	if stream.ProtocolRtmp == protocol {
		return &TransStream{}
	}

	return nil
}

func init() {
	stream.TransStreamFactory = CreateTransStream
}

func TestServer(t *testing.T) {
	impl := serverImpl{}
	addr := "0.0.0.0:1935"
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		panic(err)
	}

	err = impl.Start(tcpAddr)
	if err != nil {
		panic(err)
	}

	println("启动rtmp服务成功:" + addr)
	select {}
}
