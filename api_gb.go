package main

import (
	"fmt"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/gb28181"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/stream"
	"net"
	"net/http"
	"strings"
)

type GBForwardParams struct {
	Source string `json:"source"` //GetSourceID
	Addr   string `json:"addr"`
	SSRC   uint32 `json:"ssrc"`
	Setup  string `json:"setup"`
}

type GBSourceParams struct {
	Source string `json:"source"` //GetSourceID
	Setup  string `json:"setup"`  //active/passive
	SSRC   uint32 `json:"ssrc,omitempty"`
}

type GBConnect struct {
	Source     string `json:"source"` //GetSourceID
	RemoteAddr string `json:"remote_addr"`
}

func (api *ApiServer) OnGBSourceCreate(v *GBSourceParams, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("创建国标源: %v", v)

	// 返回收流地址
	response := &struct {
		IP   string `json:"ip"`
		Port int    `json:"port,omitempty"`
	}{}

	var err error
	// 响应错误消息
	defer func() {
		if err != nil {
			log.Sugar.Errorf(err.Error())
			httpResponse2(w, err)
		}
	}()

	source := stream.SourceManager.Find(v.Source)
	if source != nil {
		log.Sugar.Errorf("创建国标源失败, %s已经存在", v.Source)
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: fmt.Sprintf("创建国标源失败, %s已经存在", v.Source)}
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
			err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "创建国标源失败, 单端口模式下不能主动拉流"}
		} else if !tcp {
			err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "创建国标源失败, UDP不能主动拉流"}
		} else if !stream.AppConfig.GB28181.IsEnableTCP() {
			err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "创建国标源失败, 未开启TCP, UDP不能主动拉流"}
		}

		if err != nil {
			return
		}
	}

	_, port, err := gb28181.NewGBSource(v.Source, v.SSRC, tcp, active)
	if err != nil {
		err = &MalformedRequest{Code: http.StatusInternalServerError, Msg: fmt.Sprintf("创建国标源失败 err:%s", err.Error())}
		return
	}

	response.IP = stream.AppConfig.PublicIP
	response.Port = port
	httpResponseOK(w, response)
}

func (api *ApiServer) OnGBSourceConnect(v *GBConnect, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("设置国标主动拉流连接地址: %v", v)

	var err error
	defer func() {
		if err != nil {
			log.Sugar.Errorf(err.Error())
			httpResponse2(w, err)
		}
	}()

	source := stream.SourceManager.Find(v.Source)
	if source == nil {
		log.Sugar.Errorf("设置主动拉流失败, %s源不存在", v.Source)
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "gb28181 source 不存在"}
		return
	}

	activeSource, ok := source.(*gb28181.ActiveSource)
	if !ok {
		log.Sugar.Errorf("设置主动拉流失败, %s源不是Active拉流类型", v.Source)
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "gbsource 不能转为active source"}
		return
	}

	addr, err := net.ResolveTCPAddr("tcp", v.RemoteAddr)
	if err != nil {
		log.Sugar.Errorf("设置主动拉流失败, err: %s", err.Error())
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "解析连接地址失败"}
		return
	}

	err = activeSource.Connect(addr)
	if err != nil {
		log.Sugar.Errorf("设置主动拉流失败, err: %s", err.Error())
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: fmt.Sprintf("连接Server失败 err:%s", err.Error())}
		return
	}

	httpResponseOK(w, nil)
}

func (api *ApiServer) OnGBSourceForward(v *GBForwardParams, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("设置国标级联转发: %v", v)

	source := stream.SourceManager.Find(v.Source)
	if source == nil {
		log.Sugar.Infof("设置国标级联转发失败 %s源不存在", v.Source)
		w.WriteHeader(http.StatusNotFound)
	} else if source.GetType() != stream.SourceType28181 {
		log.Sugar.Infof("设置国标级联转发失败 %s源不是国标推流类型", v.Source)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var setup gb28181.SetupType
	switch strings.ToLower(v.Setup) {
	case "active":
		setup = gb28181.SetupActive
		break
	case "passive":
		setup = gb28181.SetupPassive
		break
	default:
		setup = gb28181.SetupUDP
		break
	}

	addr, _ := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	sinkId := stream.NetAddr2SinkId(addr)

	// 添加随机数
	if ipv4, ok := sinkId.(uint64); ok {
		random := uint64(utils.RandomIntInRange(0x1000, 0xFFFF0000))
		sinkId = (ipv4 & 0xFFFFFFFF00000000) | (random << 16) | (ipv4 & 0xFFFF)
	}

	sink, port, err := gb28181.NewForwardSink(v.SSRC, v.Addr, setup, sinkId, v.Source)
	if err != nil {
		log.Sugar.Errorf("设置国标级联转发 err: %s", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	source.AddSink(sink)

	log.Sugar.Infof("设置国标级联转发成功 ID: %s", sink.GetID())

	response := struct {
		ID   string `json:"id"` //sink id
		IP   string `json:"ip"`
		Port int    `json:"port"`
	}{ID: stream.SinkId2String(sinkId), IP: stream.AppConfig.PublicIP, Port: port}

	httpResponse2(w, &response)
}
