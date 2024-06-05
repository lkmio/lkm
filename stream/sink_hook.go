package stream

import (
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/log"
	"net/http"
)

func HookPlaying(s ISink, success func(), failure func(state utils.HookState)) {
	f := func() {
		source := SourceManager.Find(s.SourceId())
		if source == nil {
			log.Sugar.Infof("添加sink到等待队列 sink:%s-%v source:%s", s.Protocol().ToString(), s.Id(), s.SourceId())

			{
				s.Lock()
				defer s.UnLock()

				if SessionStateClose == s.State() {
					log.Sugar.Warnf("添加到sink到等待队列失败, sink已经断开链接 %s", s.PrintInfo())
				} else {
					s.SetState(SessionStateWait)
					AddSinkToWaitingQueue(s.SourceId(), s)
				}
			}
		} else {
			log.Sugar.Debugf("发送播放事件 sink:%s-%v source:%s", s.Protocol().ToString(), s.Id(), s.SourceId())

			source.AddEvent(SourceEventPlay, s)
		}
	}

	if !AppConfig.Hook.EnableOnPlay() {
		f()

		if success != nil {
			success()
		}
		return
	}

	err := hookEvent(HookEventPlay, NewPlayHookEventInfo(s.SourceId(), "", s.Protocol()), func(response *http.Response) {
		f()

		if success != nil {
			success()
		}
	}, func(response *http.Response, err error) {
		log.Sugar.Errorf("Hook播放事件响应失败 err:%s sink:%s-%v source:%s", err.Error(), s.Protocol().ToString(), s.Id(), s.SourceId())

		if failure != nil {
			failure(utils.HookStateFailure)
		}
	})

	if err != nil {
		log.Sugar.Errorf("Hook播放事件发送失败 err:%s sink:%s-%v source:%s", err.Error(), s.Protocol().ToString(), s.Id(), s.SourceId())

		if failure != nil {
			failure(utils.HookStateFailure)
		}
		return
	}
}

func HookPlayingDone(s ISink, success func(), failure func(state utils.HookState)) {
	if !AppConfig.Hook.EnableOnPlayDone() {
		if success != nil {
			success()
		}
		return
	}

	err := hookEvent(HookEventPlayDone, NewPlayHookEventInfo(s.SourceId(), "", s.Protocol()), func(response *http.Response) {
		if success != nil {
			success()
		}
	}, func(response *http.Response, err error) {
		log.Sugar.Errorf("Hook播放结束事件响应失败 err:%s sink:%s-%v source:%s", err.Error(), s.Protocol().ToString(), s.Id(), s.SourceId())

		if failure != nil {
			failure(utils.HookStateFailure)
		}
	})

	if err != nil {
		log.Sugar.Errorf("Hook播放结束事件发送失败 err:%s sink:%s-%v source:%s", err.Error(), s.Protocol().ToString(), s.Id(), s.SourceId())

		if failure != nil {
			failure(utils.HookStateFailure)
		}
		return
	}
}
