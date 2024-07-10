package stream

import (
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"net/http"
)

func PreparePublishSource(source Source, hook bool) (*http.Response, utils.HookState) {
	var response *http.Response

	if hook && AppConfig.Hook.EnablePublishEvent() {
		rep, state := HookPublishEvent(source)
		if utils.HookStateOK != state {
			return rep, state
		}

		response = rep
	}

	if err := SourceManager.Add(source); err != nil {
		log.Sugar.Errorf("添加源失败 source:%s err:%s", source.Id(), err.Error())
		return nil, utils.HookStateOccupy
	}

	if AppConfig.ReceiveTimeout > 0 {
		source.StartReceiveDataTimer()
	}

	if AppConfig.IdleTimeout > 0 {
		source.StartIdleTimer()
	}

	return response, utils.HookStateOK
}

func HookPublishEvent(source Source) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hook.EnablePublishEvent() {
		hook, err := Hook(HookEventPublish, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
		if err != nil {
			log.Sugar.Errorf("通知推流事件失败 source:%s err:%s", source.Id(), err.Error())
			return hook, utils.HookStateFailure
		}

		response = hook
	}

	return response, utils.HookStateOK
}

func HookPublishDoneEvent(source Source) {
	if AppConfig.Hook.EnablePublishEvent() {
		_, err := Hook(HookEventPublishDone, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
		if err != nil {
			log.Sugar.Errorf("通知推流结束事件失败 source:%s err:%s", source.Id(), err.Error())
		}
	}
}

func HookReceiveTimeoutEvent(source Source) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hook.EnableOnReceiveTimeout() {
		resp, err := Hook(HookEventReceiveTimeout, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
		if err != nil {
			log.Sugar.Errorf("通知收流超时事件失败 source:%s err:%s", source.Id(), err.Error())
			return resp, utils.HookStateFailure
		}

		response = resp
	}

	return response, utils.HookStateOK
}

func HookIdleTimeoutEvent(source Source) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hook.EnableOnIdleTimeout() {
		resp, err := Hook(HookEventIdleTimeout, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
		if err != nil {
			log.Sugar.Errorf("通知空闲超时时间失败 source:%s err:%s", source.Id(), err.Error())
			return resp, utils.HookStateFailure
		}

		response = resp
	}

	return response, utils.HookStateOK
}
