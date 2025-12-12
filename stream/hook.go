package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/lkmio/lkm/log"
	"io"
	"net/http"
	"time"
)

// 每个通知事件都需要携带的字段
type eventInfo struct {
	Stream     string `json:"stream"`      // stream GetID
	Session    string `json:"session"`     // 本次推流会话ID
	Protocol   int    `json:"protocol"`    // 推拉流协议
	RemoteAddr string `json:"remote_addr"` // peer地址
}

func responseBodyToString(resp *http.Response) string {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	return string(bodyBytes)
}

func DoPostHookEvent(event HookEvent, req *http.Request, dumpBody []byte) (*http.Response, error) {
	client := &http.Client{
		Timeout: time.Duration(AppConfig.Hooks.Timeout),
	}

	log.Sugar.Infof("sent a hook event for %s. url: %s body: %s", event.ToString(), req.URL.String(), dumpBody)
	response, err := client.Do(req)
	if err != nil {
		log.Sugar.Errorf("failed to %s the hook event. err: %s", event.ToString(), err.Error())
		return response, err
	} else {
		log.Sugar.Infof("received response for hook %s event: status='%s', response body='%s'", event.ToString(), response.Status, responseBodyToString(response))
	}

	if http.StatusOK != response.StatusCode {
		return response, fmt.Errorf("unexpected response status: %s", response.Status)
	}
	return response, nil
}

func PostHookEventWithJson(event HookEvent, params string, body interface{}) (*http.Response, error) {
	url, ok := hookUrls[event]
	if url == "" || !ok {
		return nil, fmt.Errorf("the url for this %s event does not exist", event.ToString())
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	if "" != params {
		url += "?" + params
	}

	request, err := http.NewRequest("post", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")
	return DoPostHookEvent(event, request, jsonBody)
}

func NewHookPlayEventInfo(sink Sink) eventInfo {
	return eventInfo{Stream: sink.GetSourceID(), Protocol: int(sink.GetProtocol()), RemoteAddr: sink.RemoteAddr()}
}

func NewHookPublishEventInfo(source Source) eventInfo {
	return eventInfo{Stream: source.GetID(), Session: source.GetSessionID(), Protocol: int(source.GetType()), RemoteAddr: source.RemoteAddr()}
}

func NewRecordEventInfo(source Source, path string) interface{} {
	data := struct {
		eventInfo
		Path string `json:"path"`
	}{
		eventInfo: NewHookPublishEventInfo(source),
		Path:      path,
	}

	return data
}
