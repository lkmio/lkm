package gb28181

import (
	"fmt"
	"sync"
)

const (
	MaxSsrcValue = 999999999
)

var (
	ssrcCount uint32
	lock      sync.Mutex
)

func NextSSRC() uint32 {
	lock.Lock()
	defer lock.Unlock()
	ssrcCount = (ssrcCount + 1) % MaxSsrcValue
	return ssrcCount
}

func getUniqueSSRC(ssrc string, _ func() string) string {
	return ssrc
}

func GetLiveSSRC() string {
	return getUniqueSSRC(fmt.Sprintf("0%09d", NextSSRC()), GetLiveSSRC)
}

func GetVodSSRC() string {
	return getUniqueSSRC(fmt.Sprintf("%d", 1000000000+NextSSRC()), GetVodSSRC)
}
