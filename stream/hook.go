package stream

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

type HookFunc func(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error

type Hook interface {
	DoPublish(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error

	DoPublishDone(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error

	DoPlay(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error

	DoPlayDone(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error

	DoRecord(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error

	DoIdleTimeout(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error

	DoRecvTimeout(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error
}

type HookEvent int

const (
	HookEventPublish     = HookEvent(0x1)
	HookEventPublishDone = HookEvent(0x2)
	HookEventPlay        = HookEvent(0x3)
	HookEventPlayDone    = HookEvent(0x4)
	HookEventRecord      = HookEvent(0x5)
	HookEventIdleTimeout = HookEvent(0x6)
	HookEventRecvTimeout = HookEvent(0x6)
)

// 每个通知的时间都需要携带的字段
type eventInfo struct {
	stream     string //stream id
	protocol   string //推拉流协议
	remoteAddr string //peer地址
}

func NewHookEventInfo(stream, protocol, remoteAddr string) eventInfo {
	return eventInfo{stream: stream, protocol: protocol, remoteAddr: remoteAddr}
}

type HookSession interface {
	send(url string, body interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error

	Hook(event HookEvent, body interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error
}

var hookUrls map[HookEvent]string

func init() {
	hookUrls = map[HookEvent]string{
		HookEventPublish:     "",
		HookEventPublishDone: "",
		HookEventPlay:        "",
		HookEventPlayDone:    "",
		HookEventRecord:      "",
		HookEventIdleTimeout: "",
		HookEventRecvTimeout: "",
	}
}

type hookSessionImpl struct {
}

func (h *hookSessionImpl) send(url string, body interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	marshal, err := json.Marshal(body)
	if err != nil {
		return err
	}

	client := &http.Client{
		Timeout: time.Second * time.Duration(AppConfig.Hook.Time),
	}
	request, err := http.NewRequest("post", url, bytes.NewBuffer(marshal))
	if err != nil {
		return err
	}

	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		failure(response, err)
	} else {
		success(response)
	}

	return nil
}

func (h *hookSessionImpl) Hook(event HookEvent, body interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	url := hookUrls[event]
	if url == "" {
		success(nil)
		return nil
	}

	return h.send(url, body, success, failure)
}
