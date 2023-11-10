package stream

import (
	"github.com/yangjiechina/avformat"
	"net/http"
)

// Session 封装推拉流Session 统一管理，统一 hook回调
type Session interface {
	OnPublish(source ISource, pra map[string]interface{}, success func(), failure func(state avformat.HookState))

	OnPublishDone()

	OnPlay(sink ISink, pra map[string]interface{}, success func(), failure func(state avformat.HookState))

	OnPlayDone(pra map[string]interface{}, success func(), failure func(state avformat.HookState))
}

type SessionImpl struct {
	hookImpl
	stream     string //stream id
	protocol   string //推拉流协议
	remoteAddr string //peer地址
}

// AddInfoParams 为每个需要通知的时间添加必要的信息
func (s *SessionImpl) AddInfoParams(data map[string]interface{}) {
	data["stream"] = s.stream
	data["protocol"] = s.protocol
	data["remoteAddr"] = s.remoteAddr
}

func (s *SessionImpl) OnPublish(source_ ISource, pra map[string]interface{}, success func(), failure func(state avformat.HookState)) {
	//streamId 已经被占用
	source := SourceManager.Find(s.stream)
	if source != nil {
		failure(avformat.HookStateOccupy)
		return
	}

	if !AppConfig.Hook.EnableOnPublish() {
		if err := SourceManager.Add(source_); err != nil {
			success()
		} else {
			failure(avformat.HookStateOccupy)
		}

		return
	}

	if pra == nil {
		pra = make(map[string]interface{}, 5)
	}

	s.AddInfoParams(pra)
	err := s.DoPublish(pra, func(response *http.Response) {
		if err := SourceManager.Add(source_); err != nil {
			success()
		} else {
			failure(avformat.HookStateOccupy)
		}
	}, func(response *http.Response, err error) {
		failure(avformat.HookStateFailure)
	})

	//hook地址连接失败
	if err != nil {
		failure(avformat.HookStateFailure)
		return
	}
}

func (s *SessionImpl) OnPublishDone() {

}

func (s *SessionImpl) OnPlay(sink ISink, pra map[string]interface{}, success func(), failure func(state avformat.HookState)) {
	f := func() {
		source := SourceManager.Find(s.stream)
		if source == nil {
			AddSinkToWaitingQueue(s.stream, nil)
		} else {
			source.AddSink(nil)
		}
	}

	if !AppConfig.Hook.EnableOnPlay() {
		f()
		success()
		return
	}

	if pra == nil {
		pra = make(map[string]interface{}, 5)
	}

	s.AddInfoParams(pra)
	err := s.DoPlay(pra, func(response *http.Response) {
		f()
		success()
	}, func(response *http.Response, err error) {
		failure(avformat.HookStateFailure)
	})

	if err != nil {
		failure(avformat.HookStateFailure)
		return
	}
}

func (s *SessionImpl) OnPlayDone(pra map[string]interface{}, success func(), failure func(state avformat.HookState)) {

}
