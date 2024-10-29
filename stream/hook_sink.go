package stream

import (
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"net/http"
)

func PreparePlaySink(sink Sink) (*http.Response, utils.HookState) {
	return PreparePlaySinkWithReady(sink, true)
}

func PreparePlaySinkWithReady(sink Sink, ok bool) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hooks.IsEnableOnPlay() {
		hook, err := Hook(HookEventPlay, sink.UrlValues().Encode(), NewHookPlayEventInfo(sink))
		if err != nil {
			log.Sugar.Errorf("播放事件-通知失败 err:%s sink:%s-%v source:%s", err.Error(), sink.GetProtocol().String(), sink.GetID(), sink.GetSourceID())

			return hook, utils.HookStateFailure
		}

		response = hook
	}

	sink.SetReady(ok)
	source := SourceManager.Find(sink.GetSourceID())
	if source == nil {
		log.Sugar.Infof("添加sink到等待队列 sink:%s-%v source:%s", sink.GetProtocol().String(), sink.GetID(), sink.GetSourceID())

		{
			sink.Lock()
			defer sink.UnLock()

			if SessionStateClosed == sink.GetState() {
				log.Sugar.Warnf("添加到sink到等待队列失败, sink已经断开连接 %s", sink.GetID())
				return response, utils.HookStateFailure
			} else {
				sink.SetState(SessionStateWait)
				AddSinkToWaitingQueue(sink.GetSourceID(), sink)
			}
		}
	} else {
		source.AddSink(sink)
	}

	return response, utils.HookStateOK
}

func HookPlayDoneEvent(sink Sink) (*http.Response, bool) {
	var response *http.Response

	if AppConfig.Hooks.IsEnableOnPlayDone() {
		hook, err := Hook(HookEventPlayDone, sink.UrlValues().Encode(), NewHookPlayEventInfo(sink))
		if err != nil {
			log.Sugar.Errorf("播放结束事件-通知失败 err:%s sink:%s-%v source:%s", err.Error(), sink.GetProtocol().String(), sink.GetID(), sink.GetSourceID())
			return hook, false
		}

		response = hook
	}

	return response, true
}
