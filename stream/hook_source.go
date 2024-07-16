package stream

import (
	"encoding/json"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"net/http"
)

func PreparePublishSource(source Source, hook bool) (*http.Response, utils.HookState) {
	var response *http.Response

	if hook && AppConfig.Hook.IsEnablePublishEvent() {
		rep, state := HookPublishEvent(source)
		if utils.HookStateOK != state {
			return rep, state
		}

		response = rep
	}

	if err := SourceManager.Add(source); err != nil {
		return nil, utils.HookStateOccupy
	}

	if AppConfig.ReceiveTimeout > 0 {
		source.StartReceiveDataTimer()
	}

	if AppConfig.IdleTimeout > 0 {
		source.StartIdleTimer()
	}

	urls := GetStreamPlayUrls(source.Id())
	indent, _ := json.MarshalIndent(urls, "", "\t")
	log.Sugar.Infof("%s准备推流 source:%s 拉流地址:\r\n%s", source.Type().ToString(), source.Id(), indent)

	return response, utils.HookStateOK
}

func HookPublishEvent(source Source) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hook.IsEnablePublishEvent() {
		hook, err := Hook(HookEventPublish, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
		if err != nil {
			return hook, utils.HookStateFailure
		}

		response = hook
	}

	return response, utils.HookStateOK
}

func HookPublishDoneEvent(source Source) {
	if AppConfig.Hook.IsEnablePublishEvent() {
		_, _ = Hook(HookEventPublishDone, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
	}
}

func HookReceiveTimeoutEvent(source Source) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hook.IsEnableOnReceiveTimeout() {
		resp, err := Hook(HookEventReceiveTimeout, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
		if err != nil {
			return resp, utils.HookStateFailure
		}

		response = resp
	}

	return response, utils.HookStateOK
}

func HookIdleTimeoutEvent(source Source) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hook.IsEnableOnIdleTimeout() {
		resp, err := Hook(HookEventIdleTimeout, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
		if err != nil {
			return resp, utils.HookStateFailure
		}

		response = resp
	}

	return response, utils.HookStateOK
}
