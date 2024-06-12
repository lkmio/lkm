package gb28181

import (
	"github.com/yangjiechina/lkm/stream"
	"net"
	"sync"
)

type SSRCFilter struct {
	BaseFilter

	sources map[uint32]GBSource
	mute    sync.RWMutex
}

func NewSharedFilter(guestCount int) *SSRCFilter {
	return &SSRCFilter{sources: make(map[uint32]GBSource, guestCount)}
}

func (r SSRCFilter) AddSource(ssrc uint32, source GBSource) bool {
	r.mute.Lock()
	defer r.mute.Lock()

	if _, ok := r.sources[ssrc]; !ok {
		r.sources[ssrc] = source
		return true
	}

	return false
}

func (r SSRCFilter) RemoveSource(ssrc uint32) {
	r.mute.Lock()
	defer r.mute.Lock()
	delete(r.sources, ssrc)
}

func (r SSRCFilter) Input(conn net.Conn, data []byte) GBSource {
	packet, err := r.ParseRtpPacket(conn, data)
	if err != nil {
		return nil
	}

	var source GBSource
	var ok bool
	{
		r.mute.RLock()
		source, ok = r.sources[packet.SSRC]
		r.mute.RUnlock()
	}

	if !ok {
		return nil
	}

	if stream.SessionStateHandshakeDone == source.State() {
		r.PreparePublishSource(conn, packet.SSRC, source)
	}

	source.InputRtp(packet)
	return source
}
