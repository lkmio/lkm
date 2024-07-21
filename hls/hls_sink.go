package hls

import (
	"fmt"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"strings"
	"time"
)

const (
	SessionIdKey = "hls_sid"
)

type M3U8Sink struct {
	stream.BaseSink
	cb               func(m3u8 []byte) //生成m3u8文件的发送回调
	sessionId        string
	playtime         time.Time
	playTimer        *time.Timer
	m3u8StringFormat *string
}

func (s *M3U8Sink) SendM3U8Data(data *string) error {
	s.m3u8StringFormat = data
	s.cb([]byte(s.GetM3U8String()))
	return nil
}

func (s *M3U8Sink) Start() {
	timeout := time.Duration(stream.AppConfig.IdleTimeout)
	if timeout < time.Second {
		timeout = time.Duration(stream.AppConfig.Hls.Duration) * 2 * 3 * time.Second
	}

	s.playTimer = time.AfterFunc(timeout, func() {
		sub := time.Now().Sub(s.playtime)
		if sub > timeout {
			log.Sugar.Errorf("长时间没有拉取TS切片 sink:%d 超时", s.Id_)
			s.Close()
			return
		}

		s.playTimer.Reset(timeout)
	})
}

func (s *M3U8Sink) GetM3U8String() string {
	param := fmt.Sprintf("?%s=%s", SessionIdKey, s.sessionId)
	all := strings.ReplaceAll(string(*s.m3u8StringFormat), "%s", param)
	log.Sugar.Infof("m3u8 list:%s", all)
	return all
}

func (s *M3U8Sink) RefreshPlayTime() {
	s.playtime = time.Now()
}

func (s *M3U8Sink) Close() {
	stream.SinkManager.Remove(s.Id_)
	s.BaseSink.Close()
}

func NewM3U8Sink(id stream.SinkId, sourceId string, cb func(m3u8 []byte), sessionId string) stream.Sink {
	return &M3U8Sink{
		BaseSink:  stream.BaseSink{Id_: id, SourceId_: sourceId, Protocol_: stream.ProtocolHls},
		cb:        cb,
		sessionId: sessionId,
	}
}
