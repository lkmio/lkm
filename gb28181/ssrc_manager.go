package gb28181

import (
	"fmt"
	"strconv"
	"sync"
)

const (
	MaxSsrcValue = 999999999
)

var (
	ssrcCount   uint32
	lock        sync.Mutex
	SSRCFilters []Filter
)

func NextSSRC() uint32 {
	lock.Lock()
	defer lock.Unlock()
	ssrcCount = (ssrcCount + 1) % MaxSsrcValue
	return ssrcCount
}

func getUniqueSSRC(ssrc string, get func() string) string {
	atoi, err := strconv.Atoi(ssrc)
	if err != nil {
		panic(err)
	}

	v := uint32(atoi)
	for _, filter := range SSRCFilters {
		if filter.FindSource(v) != nil {
			ssrc = get()
		}
	}

	return ssrc
}

func GetLiveSSRC() string {
	return getUniqueSSRC(fmt.Sprintf("0%09d", NextSSRC()), GetLiveSSRC)
}

func GetVodSSRC() string {
	return getUniqueSSRC(fmt.Sprintf("%d", 1000000000+NextSSRC()), GetVodSSRC)
}
