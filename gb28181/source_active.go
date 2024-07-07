package gb28181

import (
	"net"
)

type ActiveSource struct {
	PassiveSource

	port       int
	remoteAddr net.TCPAddr
	tcp        *TCPClient
}

func NewActiveSource() (*ActiveSource, int, error) {
	var port int
	TransportManger.AllocPort(true, func(port_ uint16) error {
		port = int(port_)
		return nil
	})

	return &ActiveSource{port: port}, port, nil
}

func (a ActiveSource) Connect(remoteAddr *net.TCPAddr) error {
	client, err := NewTCPClient(a.port, remoteAddr, &a)
	if err != nil {
		return err
	}

	a.tcp = client
	return nil
}

func (a ActiveSource) TransportType() TransportType {
	return TransportTypeTCPActive
}
