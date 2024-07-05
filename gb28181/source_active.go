package gb28181

import (
	"net"
)

type ActiveSource struct {
	PassiveSource

	port       uint16
	remoteAddr net.TCPAddr
	tcp        *TCPClient
}

func NewActiveSource() (*ActiveSource, uint16, error) {
	var port uint16
	TransportManger.AllocPort(true, func(port_ uint16) error {
		port = port_
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
