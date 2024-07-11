package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/lkmio/lkm/log"
	"net/http"
	"time"
)

// 每个通知事件都需要携带的字段
type eventInfo struct {
	Stream     string `json:"stream"`      //stream id
	Protocol   string `json:"protocol"`    //推拉流协议
	RemoteAddr string `json:"remote_addr"` //peer地址
}

func NewHookPlayEventInfo(sink Sink) eventInfo {
	return eventInfo{Stream: sink.SourceId(), Protocol: sink.Protocol().ToString(), RemoteAddr: sink.PrintInfo()}
}
func NewHookPublishEventInfo(source Source) eventInfo {
	return eventInfo{Stream: source.Id(), Protocol: source.Type().ToString(), RemoteAddr: source.RemoteAddr()}
}

func sendHookEvent(url string, body interface{}) (*http.Response, error) {
	marshal, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: time.Duration(AppConfig.Hook.Timeout),
	}
	request, err := http.NewRequest("post", url, bytes.NewBuffer(marshal))
	if err != nil {
		return nil, err
	}

	log.Sugar.Infof("发送hook通知 url:%s body:%s", url, marshal)
	request.Header.Set("Content-Type", "application/json")
	return client.Do(request)
}

func Hook(event HookEvent, params string, body interface{}) (*http.Response, error) {
	url, ok := hookUrls[event]
	if url == "" || !ok {
		return nil, fmt.Errorf("the url for this %s event does not exist", event.ToString())
	}

	if "" != params {
		url += "?" + params
	}

	response, err := sendHookEvent(url, body)
	if err == nil && http.StatusOK != response.StatusCode {
		return response, fmt.Errorf("reason %s", response.Status)
	}

	return response, err
}
