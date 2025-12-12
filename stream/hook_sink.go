package stream

import (
	"context"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"net/http"
	"time"
)

const (
	ForwardSinkWaitTimeout = 20
)

func PreparePlaySink(sink Sink, waitTimeout bool) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hooks.IsEnableOnPlay() {
		body := struct {
			eventInfo
			Sink string `json:"sink"`
		}{
			eventInfo: NewHookPlayEventInfo(sink),
			Sink:      SinkID2String(sink.GetID()),
		}

		hook, err := PostHookEventWithJson(HookEventPlay, sink.UrlValues().Encode(), body)
		if err != nil {
			log.Sugar.Errorf("播放事件-通知失败 err: %s sink: %s-%v source: %s", err.Error(), sink.GetProtocol().String(), sink.GetID(), sink.GetSourceID())

			return hook, utils.HookStateFailure
		}

		response = hook
	}

	source := SourceManager.Find(sink.GetSourceID())
	if source == nil {
		log.Sugar.Infof("添加%s sink到等待队列 id: %v source: %s", sink.GetProtocol().String(), sink.GetID(), sink.GetSourceID())

		{
			sink.Lock()
			defer sink.UnLock()

			if SessionStateClosed == sink.GetState() {
				log.Sugar.Warnf("添加到%s sink到等待队列失败, sink已经断开连接 %s", sink.GetProtocol(), sink.GetID())
				return response, utils.HookStateFailure
			} else {
				if waitTimeout {
					go func() {
						timeout := sink.StartWaitTimer(context.Background(), ForwardSinkWaitTimeout*time.Second)
						if timeout {
							log.Sugar.Warnf("在等待队列超时, 删除%s sink id: %v source: %s", sink.GetProtocol().String(), sink.GetID(), sink.GetSourceID())
							sink.Close()
						}
					}()
				}

				sink.SetState(SessionStateWaiting)
				AddSinkToWaitingQueue(sink.GetSourceID(), sink)
			}
		}
	} else {
		source.GetTransStreamPublisher().AddSink(sink)
	}

	return response, utils.HookStateOK
}

func HookPlayDoneEvent(sink Sink) (*http.Response, bool) {
	var response *http.Response

	if AppConfig.Hooks.IsEnableOnPlayDone() {
		body := struct {
			eventInfo
			Sink string `json:"sink"`
		}{
			eventInfo: NewHookPlayEventInfo(sink),
			Sink:      SinkID2String(sink.GetID()),
		}

		hook, err := PostHookEventWithJson(HookEventPlayDone, sink.UrlValues().Encode(), body)
		if err != nil {
			log.Sugar.Errorf("播放结束事件-通知失败 err: %s sink: %s-%v source: %s", err.Error(), sink.GetProtocol().String(), sink.GetID(), sink.GetSourceID())
			return hook, false
		}

		response = hook
	}

	return response, true
}
