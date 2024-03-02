package stream

import (
	"github.com/yangjiechina/avformat/utils"
)

type SourceHook interface {
	Publish(source ISource, success func(), failure func(state utils.HookState))

	PublishDone(source ISource, success func(), failure func(state utils.HookState))
}

type SinkHook interface {
	Play(sink ISink, success func(), failure func(state utils.HookState))

	PlayDone(source ISink, success func(), failure func(state utils.HookState))
}
