package stream

import (
	"fmt"
	"github.com/yangjiechina/avformat/libbufio"
	"github.com/yangjiechina/avformat/utils"
	"sync"
)

type TransportManager interface {
	AllocTransport(tcp bool, cb func(port uint16) error) error

	AllocPairTransport(cb, c2 func(port uint16) error) error
}

func NewTransportManager(start, end uint16) TransportManager {
	utils.Assert(end > start)

	return &transportManager{
		startPort: start,
		endPort:   end,
		nextPort:  start,
	}
}

type transportManager struct {
	startPort uint16
	endPort   uint16
	nextPort  uint16
	lock      sync.Mutex
}

func (t *transportManager) AllocTransport(tcp bool, cb func(port uint16) error) error {
	loop := func(start, end uint16, tcp bool) (uint16, error) {
		for i := start; i < end; i++ {
			if used := utils.Used(int(i), tcp); !used {
				return i, cb(i)
			}
		}

		return 0, nil
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	port, err := loop(t.nextPort, t.endPort, tcp)
	if port == 0 {
		port, err = loop(t.startPort, t.nextPort, tcp)
	}

	if port == 0 {
		return fmt.Errorf("no available ports in the [%d-%d] range", t.startPort, t.endPort)
	} else if err != nil {
		return err
	}

	t.nextPort = t.nextPort + 1%t.endPort
	t.nextPort = uint16(libbufio.MaxInt(int(t.nextPort), int(t.startPort)))
	return nil
}

func (t *transportManager) AllocPairTransport(cb func(port uint16) error, cb2 func(port uint16) error) error {
	if err := t.AllocTransport(false, cb); err != nil {
		return err
	}

	if err := t.AllocTransport(false, cb2); err != nil {
		return err
	}
	return nil
}
