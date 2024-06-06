package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/yangjiechina/avformat/utils"
	"net/http"
	"time"
)

type HookFunc func(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error
type HookEvent int

const (
	HookEventPublish     = HookEvent(0x1)
	HookEventPublishDone = HookEvent(0x2)
	HookEventPlay        = HookEvent(0x3)
	HookEventPlayDone    = HookEvent(0x4)
	HookEventRecord      = HookEvent(0x5)
	HookEventIdleTimeout = HookEvent(0x6)
	HookEventRecvTimeout = HookEvent(0x7)
)

// 每个通知的时间都需要携带的字段
type eventInfo struct {
	stream     string //stream id
	protocol   string //推拉流协议
	remoteAddr string //peer地址
}

func NewPlayHookEventInfo(stream, remoteAddr string, protocol Protocol) eventInfo {
	return eventInfo{stream: stream, protocol: protocol.ToString(), remoteAddr: remoteAddr}
}

func NewPublishHookEventInfo(stream, remoteAddr string, protocol SourceType) eventInfo {
	return eventInfo{stream: stream, protocol: protocol.ToString(), remoteAddr: remoteAddr}
}

type HookHandler interface {
	Play(success func(), failure func(state utils.HookState))

	PlayDone(success func(), failure func(state utils.HookState))
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

func sendHookEvent(url string, body interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
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
	if err != nil {
		failure(response, err)
	} else if response.StatusCode != http.StatusOK {
		failure(response, fmt.Errorf("code:%d reason:%s", response.StatusCode, response.Status))
	} else {
		success(response)
	}

	return nil
}

func hookEvent(event HookEvent, body interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	url := hookUrls[event]
	if url == "" {
		success(nil)
		return nil
	}

	return sendHookEvent(url, body, success, failure)
}

type hookSession struct {
}

func (h *hookSession) send(url string, body interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
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
	if err != nil {
		failure(response, err)
	} else if response.StatusCode != http.StatusOK {
		failure(response, fmt.Errorf("code:%d reason:%s", response.StatusCode, response.Status))
	} else {
		success(response)
	}

	return sendHookEvent(url, body, success, failure)
}

func (h *hookSession) Hook(event HookEvent, body interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	return hookEvent(event, body, success, failure)
}
