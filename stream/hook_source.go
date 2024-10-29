package stream

import (
	"encoding/json"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"net/http"
	"time"
)

func PreparePublishSource(source Source, hook bool) (*http.Response, utils.HookState) {
	var response *http.Response

	if hook && AppConfig.Hooks.IsEnablePublishEvent() {
		rep, state := HookPublishEvent(source)
		if utils.HookStateOK != state {
			return rep, state
		}

		response = rep
	}

	if err := SourceManager.Add(source); err != nil {
		return nil, utils.HookStateOccupy
	}

	if AppConfig.Hooks.IsEnableOnReceiveTimeout() && AppConfig.ReceiveTimeout > 0 {
		StartReceiveDataTimer(source)
	}

	if AppConfig.Hooks.IsEnableOnIdleTimeout() && AppConfig.IdleTimeout > 0 {
		StartIdleTimer(source)
	}

	source.SetCreateTime(time.Now())

	urls := GetStreamPlayUrls(source.GetID())
	indent, _ := json.MarshalIndent(urls, "", "\t")

	log.Sugar.Infof("%s准备推流 source:%s 拉流地址:\r\n%s", source.GetType().String(), source.GetID(), indent)

	return response, utils.HookStateOK
}

func HookPublishEvent(source Source) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hooks.IsEnablePublishEvent() {
		hook, err := Hook(HookEventPublish, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
		if err != nil {
			return hook, utils.HookStateFailure
		}

		response = hook
	}

	return response, utils.HookStateOK
}

func HookPublishDoneEvent(source Source) {
	if AppConfig.Hooks.IsEnablePublishEvent() {
		_, _ = Hook(HookEventPublishDone, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
	}
}

func HookReceiveTimeoutEvent(source Source) (*http.Response, utils.HookState) {
	var response *http.Response

	if AppConfig.Hooks.IsEnableOnReceiveTimeout() {
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

	if AppConfig.Hooks.IsEnableOnIdleTimeout() {
		resp, err := Hook(HookEventIdleTimeout, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
		if err != nil {
			return resp, utils.HookStateFailure
		}

		response = resp
	}

	return response, utils.HookStateOK
}

func HookRecordEvent(source Source, path string) {
	if AppConfig.Hooks.IsEnableOnRecord() {
		_, _ = Hook(HookEventRecord, "", NewRecordEventInfo(source, path))
	}
}
