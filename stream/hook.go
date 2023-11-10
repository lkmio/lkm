package stream

import (
	"bytes"
	"encoding/json"
	"net/http"
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

type hookImpl struct {
}

// 每个通知的时间都需要携带的字段
type eventInfo struct {
	stream     string //stream id
	protocol   string //推拉流协议
	remoteAddr string //peer地址
}

func (hook *hookImpl) send(url string, m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	marshal, err := json.Marshal(m)
	if err != nil {
		return err
	}

	client := &http.Client{}
	request, err := http.NewRequest("post", url, bytes.NewBuffer(marshal))
	if err != nil {
		return err
	}

	request.Header.Set("Content-Type", "application/json")
	go func() {
		response, err := client.Do(request)
		if err != nil || response.StatusCode != http.StatusOK {
			failure(response, err)
			return
		}

		success(response)
	}()

	return nil
}

func (hook *hookImpl) DoPublish(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	return hook.send(AppConfig.Hook.OnPublish, m, success, failure)
}

func (hook *hookImpl) DoPublishDone(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	return hook.send(AppConfig.Hook.OnPublishDone, m, success, failure)
}

func (hook *hookImpl) DoPlay(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	return hook.send(AppConfig.Hook.OnPlay, m, success, failure)
}

func (hook *hookImpl) DoPlayDone(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	return hook.send(AppConfig.Hook.OnPlayDone, m, success, failure)
}

func (hook *hookImpl) DoRecord(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	return hook.send(AppConfig.Hook.OnRecord, m, success, failure)
}

func (hook *hookImpl) DoIdleTimeout(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	return hook.send(AppConfig.Hook.OnIdleTimeout, m, success, failure)
}

func (hook *hookImpl) DoRecvTimeout(m map[string]interface{}, success func(response *http.Response), failure func(response *http.Response, err error)) error {
	return hook.send(AppConfig.Hook.OnRecvTimeout, m, success, failure)
}
