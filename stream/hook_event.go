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
	HookEventStarted        = HookEvent(0x8)
)

var (
	hookUrls map[HookEvent]string
)

func InitHookUrls() {
	hookUrls = map[HookEvent]string{
		HookEventPublish:        AppConfig.Hooks.OnPublishUrl,
		HookEventPublishDone:    AppConfig.Hooks.OnPublishDoneUrl,
		HookEventPlay:           AppConfig.Hooks.OnPlayUrl,
		HookEventPlayDone:       AppConfig.Hooks.OnPlayDoneUrl,
		HookEventRecord:         AppConfig.Hooks.OnRecordUrl,
		HookEventIdleTimeout:    AppConfig.Hooks.OnIdleTimeoutUrl,
		HookEventReceiveTimeout: AppConfig.Hooks.OnReceiveTimeoutUrl,
		HookEventStarted:        AppConfig.Hooks.OnStartedUrl,
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
	} else if HookEventStarted == *h {
		return "started"
	}

	panic(fmt.Sprintf("unknow hook type %d", h))
}
