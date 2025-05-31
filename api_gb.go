package main

import (
	"fmt"
	"github.com/lkmio/avformat/bufio"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/gb28181"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"net"
	"net/http"
	"strconv"
)

const (
	InviteTypePlay      = "play"
	InviteTypePlayback  = "playback"
	InviteTypeDownload  = "download"
	InviteTypeBroadcast = "broadcast"
	InviteTypeTalk      = "talk"
)

type SDP struct {
	SessionName string `json:"session_name,omitempty"` // play/download/playback/talk/broadcast
	Addr        string `json:"addr,omitempty"`         // 连接地址
	SSRC        string `json:"ssrc,omitempty"`
	Setup       string `json:"setup,omitempty"`     // active/passive
	Transport   string `json:"transport,omitempty"` // tcp/udp
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

	if tcp && active {
		if !stream.AppConfig.GB28181.IsMultiPort() {
			err = fmt.Errorf("单端口模式下不能主动拉流")
		} else if !tcp {
			err = fmt.Errorf("UDP不能主动拉流")
		} else if !stream.AppConfig.GB28181.IsEnableTCP() {
			err = fmt.Errorf("未开启TCP收流服务,UDP不能主动拉流")
		}

		if err != nil {
			return
		}
	}

	var ssrc string
	if v.SessionName == InviteTypeDownload || v.SessionName == InviteTypePlayback {
		ssrc = gb28181.GetVodSSRC()
	} else {
		ssrc = gb28181.GetLiveSSRC()
	}

	ssrcValue, _ := strconv.Atoi(ssrc)
	_, port, err := gb28181.NewGBSource(v.Source, uint32(ssrcValue), tcp, active)
	if err != nil {
		return
	}

	response.Addr = net.JoinHostPort(stream.AppConfig.PublicIP, strconv.Itoa(port))
	response.Urls = stream.GetStreamPlayUrls(v.Source)
	response.SSRC = ssrc

	log.Sugar.Infof("创建国标源成功, addr: %s, ssrc: %d", response.Addr, ssrcValue)
	httpResponseOK(w, response)
}

func (api *ApiServer) OnGBSourceConnect(v *SourceSDP, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("设置国标主动拉流连接地址: %v", v)

	var err error
	// 响应错误消息
	defer func() {
		if err != nil {
			log.Sugar.Errorf("设置国标主动拉流失败 err: %s", err.Error())
			httpResponseError(w, err.Error())
		}
	}()

	source := stream.SourceManager.Find(v.Source)
	if source == nil {
		err = fmt.Errorf("%s 源不存在", v.Source)
		return
	}

	activeSource, ok := source.(*gb28181.ActiveSource)
	if !ok {
		err = fmt.Errorf("%s 源不是Active拉流类型", v.Source)
		return
	}

	addr, err := net.ResolveTCPAddr("tcp", v.Addr)
	if err != nil {
		return
	}

	if err = activeSource.Connect(addr); err == nil {
		httpResponseOK(w, nil)
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

func (api *ApiServer) AddForwardSink(protocol stream.TransStreamProtocol, transport stream.TransportType, sourceId string, remoteAddr string, w http.ResponseWriter, r *http.Request) {
	var port int
	sink, port, err := stream.ForwardStream(protocol, transport, sourceId, r.URL.Query(), remoteAddr, gb28181.TransportManger)
	if err != nil {
		log.Sugar.Errorf("创建转发sink失败 err: %s", err.Error())
		httpResponseError(w, err.Error())
		return
	}

	log.Sugar.Infof("创建转发sink成功, sink: %s port: %d transport: %s", sink.GetID(), port, transport)

	response := struct {
		Sink string `json:"sink"` // sink id
		SDP
	}{Sink: stream.SinkID2String(sink.GetID()), SDP: SDP{Addr: net.JoinHostPort(stream.AppConfig.PublicIP, strconv.Itoa(port))}}

	httpResponseOK(w, &response)
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
	talkSource.Init(stream.TCPReceiveBufferQueueSize)
	talkSource.SetUrlValues(r.Form)

	_, state := stream.PreparePublishSource(talkSource, true)
	if utils.HookStateOK != state {
		log.Sugar.Errorf("对讲失败, source: %s", talkSource)
		conn.Close()
		return
	}

	log.Sugar.Infof("ws对讲连接成功, source: %s", talkSource)

	go stream.LoopEvent(talkSource)

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
			data := stream.UDPReceiveBufferPool.Get().([]byte)
			n := bufio.MinInt(stream.UDPReceiveBufferSize, length-i)
			copy(data, bytes[:n])
			_ = talkSource.PublishSource.Input(data[:n])
			i += n
		}
	}

	talkSource.Close()
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

	api.AddForwardSink(v.TransStreamProtocol, setup.TransportType(), v.Source, v.Addr, w, r)
}
