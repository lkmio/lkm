package stream

import (
	"github.com/yangjiechina/avformat/utils"
)

type SourceHook interface {
	Publish(source Source, success func(), failure func(state utils.HookState))

	PublishDone(source Source, success func(), failure func(state utils.HookState))
}

type SinkHook interface {
	Play(sink Sink, success func(), failure func(state utils.HookState))

	PlayDone(source Sink, success func(), failure func(state utils.HookState))
}
