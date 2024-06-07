package stream

import "fmt"

type HookEvent int

const (
	HookEventPublish        = HookEvent(0x1)
	HookEventPublishDone    = HookEvent(0x2)
	HookEventPlay           = HookEvent(0x3)
	HookEventPlayDone       = HookEvent(0x4)
	HookEventRecord         = HookEvent(0x5)
	HookEventIdleTimeout    = HookEvent(0x6)
	HookEventReceiveTimeout = HookEvent(0x7)
)

var (
	hookUrls map[HookEvent]string
)

func InitHookUrl() {
	hookUrls = map[HookEvent]string{
		HookEventPublish:        AppConfig.Hook.OnPublishUrl,
		HookEventPublishDone:    AppConfig.Hook.OnPublishDoneUrl,
		HookEventPlay:           AppConfig.Hook.OnPlayUrl,
		HookEventPlayDone:       AppConfig.Hook.OnPlayDoneUrl,
		HookEventRecord:         AppConfig.Hook.OnRecordUrl,
		HookEventIdleTimeout:    AppConfig.Hook.OnIdleTimeoutUrl,
		HookEventReceiveTimeout: AppConfig.Hook.OnReceiveTimeoutUrl,
	}
}

func (h *HookEvent) ToString() string {
	if HookEventPublish == *h {
		return "publish"
	} else if HookEventPublishDone == *h {
		return "publish done"
	} else if HookEventPlay == *h {
		return "play"
	} else if HookEventPlayDone == *h {
		return "play done"
	} else if HookEventRecord == *h {
		return "record"
	} else if HookEventIdleTimeout == *h {
		return "idle timeout"
	} else if HookEventReceiveTimeout == *h {
		return "receive timeout"
	}

	panic(fmt.Sprintf("unknow hook type %d", h))
}
