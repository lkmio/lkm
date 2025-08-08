package gb28181

import (
	"github.com/lkmio/lkm/stream"
	"github.com/lkmio/transport"
	"net"
)

type ActiveSource struct {
	*PassiveSource
	port       int
	remoteAddr net.TCPAddr
}

func (a *ActiveSource) Connect(remoteAddr *net.TCPAddr) error {
	client := &transport.TCPClient{}
	client.SetHandler(a.PassiveSource)

	addr, err := net.ResolveTCPAddr("tcp", stream.ListenAddr(a.port))
	if err != nil {
		return err
	} else if _, err = client.Connect(addr, remoteAddr); err != nil {
		return err
	}

	go client.Receive()
	a.transport = client
	return nil
}

func (a *ActiveSource) SetupType() SetupType {
	return SetupActive
}

func NewActiveSource() (*ActiveSource, int, error) {
	var port int
	err := TransportManger.AllocPort(true, func(port_ uint16) error {
		port = int(port_)
		return nil
	})

	if err != nil {
		return nil, 0, err
	}

	return &ActiveSource{
		PassiveSource: &PassiveSource{
			StreamServer: stream.StreamServer[GBSource]{
				SourceType: stream.SourceType28181,
			},
			decoder:       transport.NewLengthFieldFrameDecoder(0xFFFF, 2),
			receiveBuffer: stream.TCPReceiveBufferPool.Get().([]byte),
		},
		port: port,
	}, port, nil
}
