package stream

import (
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/log"
	"net/http"
)

func PreparePlaySink(sink Sink) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hook.EnableOnPlay() {
		hook, err := Hook(HookEventPlay, sink.UrlValues().Encode(), NewHookPlayEventInfo(sink))
		if err != nil {
			log.Sugar.Errorf("通知播放事件失败 err:%s sink:%s-%v source:%s", err.Error(), sink.Protocol().ToString(), sink.Id(), sink.SourceId())

			return hook, utils.HookStateFailure
		}

		response = hook
	}

	source := SourceManager.Find(sink.SourceId())
	if source == nil {
		log.Sugar.Infof("添加sink到等待队列 sink:%s-%v source:%s", sink.Protocol().ToString(), sink.Id(), sink.SourceId())

		{
			sink.Lock()
			defer sink.UnLock()

			if SessionStateClose == sink.State() {
				log.Sugar.Warnf("添加到sink到等待队列失败, sink已经断开链接 %s", sink.Id())
				return response, utils.HookStateFailure
			} else {
				sink.SetState(SessionStateWait)
				AddSinkToWaitingQueue(sink.SourceId(), sink)
			}
		}
	} else {
		source.AddEvent(SourceEventPlay, sink)
	}

	return response, utils.HookStateOK
}

func HookPlayDoneEvent(sink Sink) (*http.Response, bool) {
	var response *http.Response

	if AppConfig.Hook.EnableOnPlayDone() {
		hook, err := Hook(HookEventPlayDone, sink.UrlValues().Encode(), NewHookPlayEventInfo(sink))
		if err != nil {
			log.Sugar.Errorf("通知播放结束事件失败 err:%s sink:%s-%v source:%s", err.Error(), sink.Protocol().ToString(), sink.Id(), sink.SourceId())
			return hook, false
		}

		response = hook
	}

	return response, true
}
