package gb28181

import (
	"sync"
)

type ssrcFilter struct {
	sources map[uint32]GBSource
	mute    sync.RWMutex
}

func NewSharedFilter(guestCount int) Filter {
	return &ssrcFilter{sources: make(map[uint32]GBSource, guestCount)}
}

func (r *ssrcFilter) AddSource(ssrc uint32, source GBSource) bool {
	r.mute.Lock()
	defer r.mute.Unlock()

	if _, ok := r.sources[ssrc]; !ok {
		r.sources[ssrc] = source
		return true
	}

	return false
}

func (r *ssrcFilter) RemoveSource(ssrc uint32) {
	r.mute.Lock()
	defer r.mute.Unlock()
	delete(r.sources, ssrc)
}

func (r *ssrcFilter) FindSource(ssrc uint32) GBSource {
	r.mute.RLock()
	defer r.mute.RUnlock()
	return r.sources[ssrc]
}
