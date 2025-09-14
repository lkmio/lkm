package main

import (
	"encoding/base64"
	"fmt"
	"github.com/gorilla/mux"
	audio_transcoder "github.com/lkmio/audio-transcoder"
	"github.com/lkmio/avformat/bufio"
	"github.com/lkmio/lkm/gb28181"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"net"
	"net/http"
	"strconv"
	"time"
)

const (
	InviteTypePlay      = "play"
	InviteTypePlayback  = "playback"
	InviteTypeDownload  = "download"
	InviteTypeBroadcast = "broadcast"
	InviteTypeTalk      = "talk"
)

type SDP struct {
	SessionName string  `json:"session_name,omitempty"` // play/download/playback/talk/broadcast
	Addr        string  `json:"addr,omitempty"`         // 连接地址
	SSRC        string  `json:"ssrc,omitempty"`
	Setup       string  `json:"setup,omitempty"`     // active/passive
	Transport   string  `json:"transport,omitempty"` // tcp/udp
	Speed       float64 `json:"speed,omitempty"`
	StartTime   int     `json:"start_time,omitempty"`
	EndTime     int     `json:"end_time,omitempty"`
	FileSize    int     `json:"file_size,omitempty"`
}

type DownloadInfo struct {
	PlaybackDuration  int     // 回放/下载时长
	PlaybackSpeed     float64 // 回放/下载速度
	PlaybackFileURL   string  // 回放/下载文件URL
	PlaybackStartTime string  // 回放/下载开始时间
	PlaybackEndTime   string  // 回放/下载结束时间
	PlaybackFileSize  int     // 回放/下载文件大小
	PlaybackProgress  float64 // 1-下载完成
	Progress          float64
}

type SourceSDP struct {
	Source string `json:"source"` // GetSourceID
	SDP
}

type GBOffer struct {
	SourceSDP
	AnswerSetup         string                     `json:"answer_setup,omitempty"` // 希望应答的连接方式
	TransStreamProtocol stream.TransStreamProtocol `json:"trans_stream_protocol,omitempty"`
}

func Source2GBSource(source stream.Source) gb28181.GBSource {
	if gbSource, ok := source.(*gb28181.PassiveSource); ok {
		return gbSource
	} else if gbSource, ok := source.(*gb28181.ActiveSource); ok {
		return gbSource
	} else if gbSource, ok := source.(*gb28181.PassiveSource); ok {
		return gbSource
	}

	return nil
}

func (api *ApiServer) OnGBSourceCreate(v *SourceSDP, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("创建国标源: %v", v)

	// 返回收流地址
	response := &struct {
		SDP
		Urls []string `json:"urls"`
	}{}

	var err error
	// 响应错误消息
	defer func() {
		if err != nil {
			log.Sugar.Errorf("创建国标源失败 err: %s", err.Error())
			httpResponseError(w, err.Error())
		}
	}()

	source := stream.SourceManager.Find(v.Source)
	if source != nil {
		err = fmt.Errorf("%s 源已经存在", v.Source)
		return
	}

	tcp := true
	var active bool
	if v.Setup == "passive" {
	} else if v.Setup == "active" {
		active = true
	} else {
		tcp = false
		//udp收流
	}

	var ssrc string
	if InviteTypeDownload == v.SessionName || InviteTypePlayback == v.SessionName {
		ssrc = gb28181.GetVodSSRC()
	} else {
		ssrc = gb28181.GetLiveSSRC()
	}

	ssrcValue, _ := strconv.Atoi(ssrc)
	gbSource, port, err := gb28181.NewGBSource(v.Source, uint32(ssrcValue), tcp, active)
	if err != nil {
		return
	} else if InviteTypeDownload == v.SessionName {
		// 开启录制
		gbSource.GetTransStreamPublisher().StartRecord()
	}

	startTime := time.Unix(int64(v.StartTime), 0).Format("2006-01-02T15:04:05")
	endTime := time.Unix(int64(v.EndTime), 0).Format("2006-01-02T15:04:05")
	gbSource.SetSessionName(v.SessionName)
	gbSource.SetStartTime(startTime)
	gbSource.SetEndTime(endTime)
	gbSource.SetSpeed(v.Speed)
	gbSource.SetDuration(v.EndTime - v.StartTime)

	response.Addr = net.JoinHostPort(stream.AppConfig.PublicIP, strconv.Itoa(port))
	response.Urls = stream.GetStreamPlayUrls(v.Source)
	response.SSRC = ssrc

	log.Sugar.Infof("创建国标源成功, addr: %s, ssrc: %d", response.Addr, ssrcValue)
	httpResponseOK(w, response)
}

func (api *ApiServer) OnGBSourceConnect(v *SourceSDP, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("设置国标应答: %v", v)

	var err error
	// 响应错误消息
	defer func() {
		if err != nil {
			log.Sugar.Errorf("设置国标应答失败 err: %s", err.Error())
			httpResponseError(w, err.Error())
		}
	}()

	source := stream.SourceManager.Find(v.Source)
	if source == nil {
		err = fmt.Errorf("%s 源不存在", v.Source)
	} else if stream.SourceType28181 != source.GetType() {
		err = fmt.Errorf("%s 源不是28181类型", v.Source)
	} else if activeSource, ok := source.(*gb28181.ActiveSource); ok {
		activeSource.SetFileSize(v.FileSize)
		// 主动连接取流
		var addr *net.TCPAddr
		addr, err = net.ResolveTCPAddr("tcp", v.Addr)
		if err != nil {
			return
		}

		if err = activeSource.Connect(addr); err == nil {
			httpResponseOK(w, nil)
		}
	} else if passiveSource, ok := source.(*gb28181.PassiveSource); ok {
		passiveSource.SetFileSize(v.FileSize)
	} else if udpSource, ok := source.(*gb28181.UDPSource); ok {
		udpSource.SetFileSize(v.FileSize)
	}
}

func (api *ApiServer) OnGBOfferCreate(v *SourceSDP, w http.ResponseWriter, r *http.Request) {
	// 预览下级设备
	if v.SessionName == "" || v.SessionName == InviteTypePlay ||
		v.SessionName == InviteTypePlayback ||
		v.SessionName == InviteTypeDownload {
		api.OnGBSourceCreate(v, w, r)
	} else {
		// 向上级转发广播和对讲, 或者是向设备发送invite talk
	}
}

func (api *ApiServer) AddForwardSink(protocol stream.TransStreamProtocol, transport stream.TransportType, sourceId string, remoteAddr string, ssrc, sessionName string, w http.ResponseWriter, r *http.Request) {
	// 解析或生成应答的ssrc
	var ssrcOffer int
	var ssrcAnswer string
	if ssrc != "" {
		var err error
		ssrcOffer, err = strconv.Atoi(ssrc)
		if err != nil {
			log.Sugar.Errorf("解析ssrc失败 err: %s ssrc: %s", err.Error(), ssrc)
		} else {
			ssrcAnswer = ssrc
		}
	}

	if ssrcAnswer == "" {
		if "download" != sessionName && "playback" != sessionName {
			ssrcAnswer = gb28181.GetLiveSSRC()
		} else {
			ssrcAnswer = gb28181.GetVodSSRC()
		}

		var err error
		ssrcOffer, err = strconv.Atoi(ssrcAnswer)
		// 严重错误, 直接panic
		if err != nil {
			panic(err)
		}
	}

	var port int
	sink, port, err := stream.ForwardStream(protocol, transport, sourceId, r.URL.Query(), remoteAddr, gb28181.TransportManger, uint32(ssrcOffer))
	if err != nil {
		log.Sugar.Errorf("创建转发sink失败 err: %s", err.Error())
		httpResponseError(w, err.Error())
		return
	}

	log.Sugar.Infof("创建转发sink成功, sink: %s port: %d transport: %s ssrc: %s", sink.GetID(), port, transport, ssrcAnswer)

	response := struct {
		Sink string `json:"sink"` // sink id
		SDP
	}{Sink: stream.SinkID2String(sink.GetID()), SDP: SDP{Addr: net.JoinHostPort(stream.AppConfig.PublicIP, strconv.Itoa(port)), SSRC: ssrcAnswer}}

	httpResponseOK(w, &response)
}

func (api *ApiServer) OnSinkAdd(v *GBOffer, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("添加sink: %v", *v)
	if stream.TransStreamGBCascaded != v.TransStreamProtocol && stream.TransStreamGBTalk != v.TransStreamProtocol && stream.TransStreamGBGateway != v.TransStreamProtocol {
		httpResponseError(w, "不支持的协议")
		return
	}

	setup := gb28181.SetupTypeFromString(v.Setup)
	if v.AnswerSetup != "" {
		setup = gb28181.SetupTypeFromString(v.AnswerSetup)
	}

	api.AddForwardSink(v.TransStreamProtocol, setup.TransportType(), v.Source, v.Addr, v.SSRC, v.SessionName, w, r)
}

// OnGBTalk 国标广播/对讲流程:
// 1. 浏览器使用WS携带source_id访问/api/v1/gb28181/talk, 如果source_id冲突, 直接断开ws连接
// 2. WS链接建立后, 调用gb-cms接口/api/v1/broadcast/invite, 向设备发送广播请求
func (api *ApiServer) OnGBTalk(w http.ResponseWriter, r *http.Request) {
	conn, err := api.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Sugar.Errorf("升级为websocket失败 err: %s", err.Error())
		conn.Close()
		return
	}

	// 获取id
	id := r.FormValue("source")

	talkSource := gb28181.NewTalkSource(id, conn)
	talkSource.Init()
	talkSource.SetUrlValues(r.Form)

	_, err = stream.PreparePublishSource(talkSource, true)
	if err != nil {
		log.Sugar.Errorf("对讲失败, err: %s source: %s", err, talkSource)
		conn.Close()
		return
	}

	log.Sugar.Infof("ws对讲连接成功, source: %s", talkSource)

	stream.LoopEvent(talkSource)

	data := stream.UDPReceiveBufferPool.Get().([]byte)

	for {
		_, bytes, err := conn.ReadMessage()
		length := len(bytes)
		if err != nil {
			log.Sugar.Errorf("读取对讲音频包失败, source: %s err: %s", id, err.Error())
			break
		} else if length < 1 {
			continue
		}

		for i := 0; i < length; {
			n := bufio.MinInt(stream.UDPReceiveBufferSize, length-i)
			copy(data, bytes[:n])
			_, _ = talkSource.PublishSource.Input(data[:n])
			i += n
		}
	}

	talkSource.Close()
}

// OnLiveGBSTalk liveGBS前端对讲
func (api *ApiServer) OnLiveGBSTalk(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	device := vars["device"]
	channel := vars["channel"]
	_ = r.URL.Query().Get("format")

	conn, err := api.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Sugar.Errorf("升级为websocket失败 err: %s", err.Error())
		conn.Close()
		return
	}

	// 获取id
	id := device + "/" + channel + ".broadcast"

	talkSource := gb28181.NewTalkSource(id, conn)
	talkSource.Init()
	talkSource.SetUrlValues(r.Form)

	_, err = stream.PreparePublishSource(talkSource, true)
	if err != nil {
		log.Sugar.Errorf("对讲失败, err: %s source: %s", err, talkSource)
		conn.Close()
		return
	}

	log.Sugar.Infof("ws对讲连接成功, source: %s", talkSource)

	stream.LoopEvent(talkSource)

	data := stream.UDPReceiveBufferPool.Get().([]byte)
	pcm := make([]byte, 32000)
	g711aPacket := make([]byte, stream.UDPReceiveBufferSize/2)

	for {
		_, bytes, err := conn.ReadMessage()
		length := len(bytes)
		if err != nil {
			log.Sugar.Errorf("读取对讲音频包失败, source: %s err: %s", id, err.Error())
			break
		} else if length < 1 {
			continue
		}

		// 扩容
		if int(float64(len(bytes))*1.4) > len(pcm) {
			pcm = make([]byte, len(bytes)*2)
		}

		// base64解密
		var pcmN int
		pcmN, err = base64.StdEncoding.Decode(bytes, pcm)
		if err == nil {
			log.Sugar.Errorf(err.Error())
			continue
		}

		for i := 0; i < pcmN; {
			// 控制每包大小
			n := bufio.MinInt(stream.UDPReceiveBufferSize, length-i)
			copy(data, pcm[:n])

			// 编码成G711A
			audio_transcoder.EncodeAlawToBuffer(data, g711aPacket)

			_, _ = talkSource.PublishSource.Input(g711aPacket[:n/2])
			i += n
		}
	}

	talkSource.Close()
}

func (api *ApiServer) OnGBSpeedSet(v *SourceSDP, w http.ResponseWriter, r *http.Request) {
	source := stream.SourceManager.Find(v.Source)
	if source == nil {
		w.WriteHeader(http.StatusBadRequest)
		httpResponseError(w, "stream not found")
	} else if stream.SourceType28181 != source.GetType() {
		w.WriteHeader(http.StatusBadRequest)
		httpResponseError(w, "stream type not support")
	} else if gbSource := Source2GBSource(source); gbSource != nil {
		gbSource.SetSpeed(v.Speed)
	}
}
