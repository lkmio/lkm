package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/flv"
	"github.com/yangjiechina/lkm/gb28181"
	"github.com/yangjiechina/lkm/hls"
	"github.com/yangjiechina/lkm/log"
	"github.com/yangjiechina/lkm/rtc"
	"github.com/yangjiechina/lkm/stream"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type ApiServer struct {
	upgrader *websocket.Upgrader
	router   *mux.Router
}

var apiServer *ApiServer

func init() {
	apiServer = &ApiServer{
		upgrader: &websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},

		router: mux.NewRouter(),
	}
}

func withCheckParams(f func(sourceId string, w http.ResponseWriter, req *http.Request), suffix string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		source, err := stream.Path2SourceId(req.URL.Path, suffix)
		if err != nil {
			httpResponse(w, http.StatusBadRequest, err.Error())
			return
		}

		f(source, w, req)
	}
}

func startApiServer(addr string) {
	/**
	  http://host:port/xxx.flv
	  http://host:port/xxx.rtc
	  http://host:port/xxx.m3u8
	  http://host:port/xxx_0.ts
	  ws://host:port/xxx.flv
	*/
	apiServer.router.HandleFunc("/{source}.flv", withCheckParams(apiServer.onFlv, ".flv"))
	apiServer.router.HandleFunc("/{source}/{stream}.flv", withCheckParams(apiServer.onFlv, ".flv"))
	apiServer.router.HandleFunc("/{source}.m3u8", withCheckParams(apiServer.onHLS, ".m3u8"))
	apiServer.router.HandleFunc("/{source}/{stream}.m3u8", withCheckParams(apiServer.onHLS, ".m3u8"))
	apiServer.router.HandleFunc("/{source}.ts", withCheckParams(apiServer.onTS, ".ts"))
	apiServer.router.HandleFunc("/{source}/{stream}.ts", withCheckParams(apiServer.onTS, ".ts"))
	apiServer.router.HandleFunc("/{source}.rtc", withCheckParams(apiServer.onRtc, ".rtc"))
	apiServer.router.HandleFunc("/{source}/{stream}.rtc", withCheckParams(apiServer.onRtc, ".rtc"))

	apiServer.router.HandleFunc("/v1/gb28181/source/create", apiServer.createGBSource)
	//TCP主动,设置连接地址
	apiServer.router.HandleFunc("/v1/gb28181/source/connect", apiServer.connectGBSource)
	apiServer.router.HandleFunc("/v1/gb28181/source/close", apiServer.closeGBSource)

	apiServer.router.HandleFunc("/rtc.html", func(writer http.ResponseWriter, request *http.Request) {
		http.ServeFile(writer, request, "./rtc.html")
	})
	http.Handle("/", apiServer.router)

	srv := &http.Server{
		Handler: apiServer.router,
		Addr:    addr,
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 30 * time.Second,
		ReadTimeout:  30 * time.Second,
	}

	err := srv.ListenAndServe()

	if err != nil {
		panic(err)
	}
}

func (api *ApiServer) createGBSource(w http.ResponseWriter, r *http.Request) {
	//请求参数
	v := &struct {
		Source    string `json:"source"` //SourceId
		Transport string `json:"transport,omitempty"`
		Setup     string `json:"setup"` //active/passive
		SSRC      uint32 `json:"ssrc,omitempty"`
	}{}

	//返回监听的端口
	response := &struct {
		Port uint16 `json:"port,omitempty"`
	}{}

	var err error
	defer func() {
		if err != nil {
			log.Sugar.Errorf(err.Error())
			httpResponse2(w, err)
		}
	}()

	if err = HttpDecodeJSONBody(w, r, v); err != nil {
		return
	}

	log.Sugar.Infof("gb create:%v", v)

	source := stream.SourceManager.Find(v.Source)
	if source != nil {
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "gbsource 已经存在"}
		return
	}

	tcp := strings.Contains(v.Transport, "tcp")
	var active bool
	if tcp && "active" == v.Setup {
		if !stream.AppConfig.GB28181.IsMultiPort() {
			err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "创建GB28181 Source失败, 单端口模式下不能主动拉流"}
		} else if !tcp {
			err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "创建GB28181 Source失败, UDP不能主动拉流"}
		} else if !stream.AppConfig.GB28181.EnableTCP() {
			err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "创建GB28181 Source失败, 未开启TCP, UDP不能主动拉流"}
		}

		if err != nil {
			return
		}

		active = true
	}

	_, port, err := gb28181.NewGBSource(v.Source, v.SSRC, tcp, active)
	if err != nil {
		err = &MalformedRequest{Code: http.StatusInternalServerError, Msg: fmt.Sprintf("创建GB28181 Source失败 err:%s", err.Error())}
		return
	}

	response.Port = port
	httpResponseOk(w, response)
}

func (api *ApiServer) connectGBSource(w http.ResponseWriter, r *http.Request) {
	//请求参数
	v := &struct {
		Source     string `json:"source"` //SourceId
		RemoteAddr string `json:"remote_addr"`
	}{}

	var err error
	defer func() {
		if err != nil {
			log.Sugar.Errorf(err.Error())
			httpResponse2(w, err)
		}
	}()

	if err = HttpDecodeJSONBody(w, r, v); err != nil {
		return
	}

	log.Sugar.Infof("gb connect:%v", v)

	source := stream.SourceManager.Find(v.Source)
	if source == nil {
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "gb28181 source 不存在"}
		return
	}

	activeSource, ok := source.(*gb28181.ActiveSource)
	if !ok {
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "gbsource 不能转为active source"}
		return
	}

	addr, err := net.ResolveTCPAddr("tcp", v.RemoteAddr)
	if err != nil {
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "解析连接地址失败"}
		return
	}

	err = activeSource.Connect(addr)
	if err != nil {
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: fmt.Sprintf("连接Server失败 err:%s", err.Error())}
		return
	}

	httpResponseOk(w, nil)
}

func (api *ApiServer) closeGBSource(w http.ResponseWriter, r *http.Request) {
	//请求参数
	v := &struct {
		Source string `json:"source"` //SourceId
	}{}

	var err error
	defer func() {
		if err != nil {
			log.Sugar.Errorf(err.Error())
			httpResponse2(w, err)
		}
	}()

	if err = HttpDecodeJSONBody(w, r, v); err != nil {
		httpResponse2(w, err)
		return
	}

	log.Sugar.Infof("gb close:%v", v)

	source := stream.SourceManager.Find(v.Source)
	if source == nil {
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "gb28181 source 不存在"}
		return
	}

	source.Close()
	httpResponseOk(w, nil)
}

func (api *ApiServer) generateSinkId(remoteAddr string) stream.SinkId {
	tcpAddr, err := net.ResolveTCPAddr("tcp", remoteAddr)
	if err != nil {
		panic(err)
	}

	return stream.GenerateSinkId(tcpAddr)
}

func (api *ApiServer) generateSourceId(remoteAddr string) stream.SinkId {
	tcpAddr, err := net.ResolveTCPAddr("tcp", remoteAddr)
	if err != nil {
		panic(err)
	}

	return stream.GenerateSinkId(tcpAddr)
}

func (api *ApiServer) onFlv(sourceId string, w http.ResponseWriter, r *http.Request) {
	ws := true
	if !("upgrade" == strings.ToLower(r.Header.Get("Connection"))) {
		ws = false
	} else if !("websocket" == strings.ToLower(r.Header.Get("Upgrade"))) {
		ws = false
	} else if !("13" == r.Header.Get("Sec-Websocket-Version")) {
		ws = false
	}

	if ws {
		apiServer.onWSFlv(sourceId, w, r)
	} else {
		apiServer.onHttpFLV(sourceId, w, r)
	}
}
func (api *ApiServer) onWSFlv(sourceId string, w http.ResponseWriter, r *http.Request) {
	conn, err := api.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Sugar.Errorf("websocket头检查失败 err:%s", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	sink := flv.NewFLVSink(api.generateSinkId(r.RemoteAddr), sourceId, flv.NewWSConn(conn))
	log.Sugar.Infof("ws-flv 连接 sink:%s", sink.PrintInfo())

	_, state := stream.PreparePlaySink(sink)
	if utils.HookStateOK != state {
		log.Sugar.Warnf("ws-flv 播放失败 sink:%s", sink.PrintInfo())
		w.WriteHeader(http.StatusForbidden)
		return
	}

	netConn := conn.NetConn()
	bytes := make([]byte, 64)
	for {
		if _, err := netConn.Read(bytes); err != nil {
			log.Sugar.Infof("ws-flv 断开连接 sink:%s", sink.PrintInfo())
			sink.Close()
			break
		}
	}
}

func (api *ApiServer) onHttpFLV(sourceId string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	conn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sink := flv.NewFLVSink(api.generateSinkId(r.RemoteAddr), sourceId, conn)
	log.Sugar.Infof("http-flv 连接 sink:%s", sink.PrintInfo())

	_, state := stream.PreparePlaySink(sink)
	if utils.HookStateOK != state {
		log.Sugar.Warnf("http-flv 播放失败 sink:%s", sink.PrintInfo())

		w.WriteHeader(http.StatusForbidden)
		return
	}

	bytes := make([]byte, 64)
	for {
		if _, err := conn.Read(bytes); err != nil {
			log.Sugar.Infof("http-flv 断开连接 sink:%s", sink.PrintInfo())
			sink.Close()
			break
		}
	}
}

func (api *ApiServer) onTS(source string, w http.ResponseWriter, r *http.Request) {
	if !stream.AppConfig.Hls.Enable {
		log.Sugar.Warnf("处理m3u8请求失败 server未开启hls request:%s", r.URL.Path)
		http.Error(w, "hls disable", http.StatusInternalServerError)
		return
	}

	index := strings.LastIndex(source, "_")
	if index < 0 || index == len(source)-1 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	seq := source[index+1:]
	sourceId := source[:index]
	tsPath := stream.AppConfig.Hls.TSPath(sourceId, seq)
	if _, err := os.Stat(tsPath); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	//链路复用无法获取http断开回调
	//Hijack需要自行解析http
	http.ServeFile(w, r, tsPath)
}

func (api *ApiServer) onHLS(sourceId string, w http.ResponseWriter, r *http.Request) {
	if !stream.AppConfig.Hls.Enable {
		log.Sugar.Warnf("处理hls请求失败 server未开启hls")
		http.Error(w, "hls disable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	//m3u8和ts会一直刷新, 每个请求只hook一次.
	sinkId := api.generateSinkId(r.RemoteAddr)

	//hook成功后, 如果还没有m3u8文件，等生成m3u8文件
	//后续直接返回当前m3u8文件
	if stream.SinkManager.Exist(sinkId) {
		http.ServeFile(w, r, stream.AppConfig.Hls.M3U8Path(sourceId))
	} else {
		context := r.Context()
		done := make(chan int, 0)

		sink := hls.NewM3U8Sink(sinkId, sourceId, func(m3u8 []byte) {
			w.Write(m3u8)
			done <- 0
		})

		_, state := stream.PreparePlaySink(sink)
		if utils.HookStateOK != state {
			log.Sugar.Warnf("m3u8 请求失败 sink:%s", sink.PrintInfo())

			w.WriteHeader(http.StatusForbidden)
			return
		} else {
			err := stream.SinkManager.Add(sink)
			utils.Assert(err == nil)
		}

		select {
		case <-done:
		case <-context.Done():
			break
		}
	}
}

func (api *ApiServer) onRtc(sourceId string, w http.ResponseWriter, r *http.Request) {
	v := struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	}{}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		log.Sugar.Errorf("rtc请求错误 err:%s remote:%s", err.Error(), r.RemoteAddr)

		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if err := json.Unmarshal(data, &v); err != nil {
		log.Sugar.Errorf("rtc请求错误 err:%s remote:%s", err.Error(), r.RemoteAddr)

		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	group := sync.WaitGroup{}
	group.Add(1)
	sink := rtc.NewSink(api.generateSinkId(r.RemoteAddr), sourceId, v.SDP, func(sdp string) {
		response := struct {
			Type string `json:"type"`
			SDP  string `json:"sdp"`
		}{
			Type: "answer",
			SDP:  sdp,
		}

		marshal, err := json.Marshal(response)
		if err != nil {
			panic(err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(marshal)

		group.Done()
	})

	log.Sugar.Infof("rtc 请求 sink:%s sdp:%v", sink.PrintInfo(), v.SDP)

	_, state := stream.PreparePlaySink(sink)
	if utils.HookStateOK != state {
		log.Sugar.Warnf("rtc 播放失败 sink:%s", sink.PrintInfo())

		w.WriteHeader(http.StatusForbidden)
		group.Done()
	}

	group.Wait()
}
