package rtsp

import (
	"fmt"
	"github.com/yangjiechina/avformat/utils"
)

type TransportManager interface {
	init(startPort, endPort int)

	AllocTransport(tcp bool, cb func(port int)) error

	AllocPairTransport(cb func(port int)) error
}

var rtspTransportManger transportManager

func init() {
	rtspTransportManger = transportManager{}
	rtspTransportManger.init(20000, 30000)
}

type transportManager struct {
	startPort int
	endPort   int
	nextPort  int
}

func (t *transportManager) init(startPort, endPort int) {
	utils.Assert(endPort > startPort)
	t.startPort = startPort
	t.endPort = endPort + 1
	t.nextPort = startPort
}

func (t *transportManager) AllocTransport(tcp bool, cb func(port int)) error {
	loop := func(start, end int, tcp bool) int {
		for i := start; i < end; i++ {
			if used := utils.Used(i, tcp); !used {
				cb(i)
				return i
			}
		}
		return -1
	}

	port := loop(t.nextPort, t.endPort, tcp)
	if port == -1 {
		port = loop(t.startPort, t.nextPort, tcp)
	}

	if port == -1 {
		return fmt.Errorf("no available ports in the [%d-%d] range", t.startPort, t.endPort)
	}

	t.nextPort = t.nextPort + 1%t.endPort
	t.nextPort = utils.MaxInt(t.nextPort, t.startPort)
	return nil
}

func (t *transportManager) AllocPairTransport(cb func(port int), cb2 func(port int)) error {
	if err := t.AllocTransport(false, cb); err != nil {
		return err
	}

	if err := t.AllocTransport(false, cb2); err != nil {
		return err
	}
	return nil
}
