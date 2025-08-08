package stream

import (
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
		rep, err := HookPublishEvent(source)
		if err != nil {
			_, _ = SourceManager.Remove(source.GetID())
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

func HookPublishEvent(source Source) (*http.Response, error) {
	if AppConfig.Hooks.IsEnablePublishEvent() {
		return Hook(HookEventPublish, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
	}

	return nil, nil
}

func HookPublishDoneEvent(source Source) {
	if AppConfig.Hooks.IsEnablePublishEvent() {
		_, _ = Hook(HookEventPublishDone, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
	}
}

func HookReceiveTimeoutEvent(source Source) (*http.Response, error) {
	utils.Assert(AppConfig.Hooks.IsEnableOnReceiveTimeout())
	return Hook(HookEventReceiveTimeout, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
}

func HookIdleTimeoutEvent(source Source) (*http.Response, error) {
	utils.Assert(AppConfig.Hooks.IsEnableOnIdleTimeout())
	return Hook(HookEventIdleTimeout, source.UrlValues().Encode(), NewHookPublishEventInfo(source))
}

func HookRecordEvent(source Source, path string) {
	if AppConfig.Hooks.IsEnableOnRecord() {
		_, _ = Hook(HookEventRecord, "", NewRecordEventInfo(source, path))
	}
}
