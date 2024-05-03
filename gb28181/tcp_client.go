package gb28181

import (
	"fmt"
	"github.com/yangjiechina/avformat/transport"
	"github.com/yangjiechina/live-server/stream"
	"net"
)

type TCPClient struct {
	TCPServer
}

func NewTCPClient(listenPort uint16, remoteAddr *net.TCPAddr, source GBSource) (*TCPClient, error) {
	client := &TCPClient{
		TCPServer{filter: NewSingleFilter(source)},
	}
	tcp := transport.TCPClient{}
	tcp.SetHandler(client)

	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", stream.AppConfig.GB28181.Addr, listenPort))
	if err != nil {
		return client, err
	}

	err = tcp.Connect(addr, remoteAddr)
	return client, err
}
