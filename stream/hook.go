package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// 每个通知的时间都需要携带的字段
type eventInfo struct {
	stream     string //stream id
	protocol   string //推拉流协议
	remoteAddr string //peer地址
}

func NewHookPlayEventInfo(sink Sink) eventInfo {
	return eventInfo{stream: sink.SourceId(), protocol: sink.Protocol().ToString(), remoteAddr: sink.PrintInfo()}
}
func NewHookPublishEventInfo(source Source) eventInfo {
	return eventInfo{stream: source.Id(), protocol: source.Type().ToString(), remoteAddr: source.RemoteAddr()}
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

	request.Header.Set("Content-Type", "application/json")
	return client.Do(request)
}

func Hook(event HookEvent, body interface{}) (*http.Response, error) {
	url, ok := hookUrls[event]
	if url == "" || !ok {
		return nil, fmt.Errorf("the url for this %s event does not exist", event.ToString())
	}

	response, err := sendHookEvent(url, body)
	if err != nil && http.StatusOK != response.StatusCode {
		return response, fmt.Errorf("code:%d reason:%s", response.StatusCode, response.Status)
	}

	return response, err
}
