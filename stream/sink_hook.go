package stream

import (
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/log"
	"net/http"
)

func HookPlaying(s ISink, success func(), failure func(state utils.HookState)) {
	f := func() {
		source := SourceManager.Find(s.SourceId())
		if source == nil {
			log.Sugar.Infof("添加sink到等待队列 sink:%s-%v source:%s", s.ProtocolStr(), s.Id(), s.SourceId())

			s.SetState(SessionStateWait)
			AddSinkToWaitingQueue(s.SourceId(), s)
		} else {
			log.Sugar.Debugf("发送播放事件 sink:%s-%v source:%s", s.ProtocolStr(), s.Id(), s.SourceId())

			source.AddEvent(SourceEventPlay, s)
		}
	}

	if !AppConfig.Hook.EnableOnPlay() {
		f()
		success()
		return
	}

	err := hookEvent(HookEventPlay, NewPlayHookEventInfo(s.SourceId(), "", s.Protocol()), func(response *http.Response) {
		f()
		success()
	}, func(response *http.Response, err error) {
		log.Sugar.Errorf("Hook播放事件响应失败 err:%s sink:%s-%v source:%s", err.Error(), s.ProtocolStr(), s.Id(), s.SourceId())

		failure(utils.HookStateFailure)
	})

	if err != nil {
		log.Sugar.Errorf("Hook播放事件发送失败 err:%s sink:%s-%v source:%s", err.Error(), s.ProtocolStr(), s.Id(), s.SourceId())

		failure(utils.HookStateFailure)
		return
	}
}
