package rtmp

import (
	"net"
	"testing"
)

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
