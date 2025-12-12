package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/log"
	"net/http"
	"time"
)

func AddSource(source Source) error {
	err := SourceManager.add(source)
	if err == nil {
		source.SetState(SessionStateHandshakeSuccess)
	}

	return err
}

func PreparePublishSource(source Source, add bool) (*http.Response, error) {
	var response *http.Response

	if add {
		if err := AddSource(source); err != nil {
			return nil, err
		}
	} else if SourceManager.Find(source.GetID()) == nil {
		return nil, fmt.Errorf("not found")
	}

	if AppConfig.Hooks.IsEnablePublishEvent() {
		rep, err := NotifyPublishEvent(source)
		if err != nil {
			unPassedSource := SourceManager.Find(source.GetID())
			if unPassedSource != nil {
				unPassedSource.Close()
			}
			return rep, err
		}

		response = rep
	}

	// 此时才认为source推流成功
	source.SetState(SessionStateTransferring)
	source.SetCreateTime(time.Now())

	urls := GetStreamPlayUrls(source.GetID())
	indent, _ := json.MarshalIndent(urls, "", "\t")

	log.Sugar.Infof("%s推流 source: %s 拉流地址:\r\n%s", source.GetType().String(), source.GetID(), indent)

	return response, nil
}

func PreparePublishSourceWithAsync(source Source, add bool) {
	go func() {
		var err error
		// 加锁执行, 保证并发安全
		source.ExecuteWithDeleteLock(func() {
			if source.IsClosed() {
				err = fmt.Errorf("source is closed")
			} else if _, err = PreparePublishSource(source, add); err == nil {
			}
		})

		if err != nil {
			log.Sugar.Errorf("GB28181推流失败 err: %s source: %s", err.Error(), source.GetID())

			if !source.IsClosed() {
				source.Close()
			}
		}
	}()

}

func NotifyPublishEvent(source Source) (*http.Response, error) {
	if AppConfig.Hooks.IsEnablePublishEvent() {
		return PostHookEventWithJson(HookEventPublish, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
	}

	return nil, nil
}

func NotifyPublishDoneEvent(source Source) {
	if AppConfig.Hooks.IsEnablePublishEvent() {
		_, _ = PostHookEventWithJson(HookEventPublishDone, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
	}
}

func NotifyReceiveTimeoutEvent(source Source) (*http.Response, error) {
	utils.Assert(AppConfig.Hooks.IsEnableOnReceiveTimeout())
	return PostHookEventWithJson(HookEventReceiveTimeout, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
}

func NotifyIdleTimeoutEvent(source Source) (*http.Response, error) {
	utils.Assert(AppConfig.Hooks.IsEnableOnIdleTimeout())
	return PostHookEventWithJson(HookEventIdleTimeout, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
}

func NotifyRecordEvent(source Source, path string) {
	if AppConfig.Hooks.IsEnableOnRecord() {
		_, _ = PostHookEventWithJson(HookEventRecord, "", NewRecordEventInfo(source, path))
	}
}

func NotifySnapshotEvent(source Source, codec string, keyFrameData []byte) {
	if AppConfig.Hooks.IsEnableOnSnapshot() {
		req, _ := http.NewRequest("POST", AppConfig.Hooks.OnSnapshotUrl, bytes.NewReader(keyFrameData))

		req.Header.Add("stream", source.GetID())
		req.Header.Add("session", source.GetSessionID())
		req.Header.Add("protocol", source.GetType().String())
		req.Header.Add("remote_addr", source.RemoteAddr())
		req.Header.Add("codec", codec)
		req.Header.Set("Content-Type", "application/octet-stream")

		_, _ = DoPostHookEvent(HookEventSnapshot, req, nil)
	}
}
