package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/flv"
	"github.com/lkmio/lkm/gb28181"
	"github.com/lkmio/lkm/hls"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/rtc"
	"github.com/lkmio/lkm/stream"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
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
	//{source}.flv和/{source}/{stream}.flv意味着, 推流id(路径)只能一层
	apiServer.router.HandleFunc("/{source}.flv", withCheckParams(apiServer.onFlv, ".flv"))
	apiServer.router.HandleFunc("/{source}/{stream}.flv", withCheckParams(apiServer.onFlv, ".flv"))
	apiServer.router.HandleFunc("/{source}.m3u8", withCheckParams(apiServer.onHLS, ".m3u8"))
	apiServer.router.HandleFunc("/{source}/{stream}.m3u8", withCheckParams(apiServer.onHLS, ".m3u8"))
	apiServer.router.HandleFunc("/{source}.ts", withCheckParams(apiServer.onTS, ".ts"))
	apiServer.router.HandleFunc("/{source}/{stream}.ts", withCheckParams(apiServer.onTS, ".ts"))
	apiServer.router.HandleFunc("/{source}.rtc", withCheckParams(apiServer.onRtc, ".rtc"))
	apiServer.router.HandleFunc("/{source}/{stream}.rtc", withCheckParams(apiServer.onRtc, ".rtc"))

	apiServer.router.HandleFunc("/api/v1/gb28181/source/create", apiServer.createGBSource)
	//TCP主动,设置连接地址
	apiServer.router.HandleFunc("/api/v1/gb28181/source/connect", apiServer.connectGBSource)
	apiServer.router.HandleFunc("/api/v1/gb28181/source/close", apiServer.closeGBSource)
	apiServer.router.HandleFunc("/api/v1/gc/force", func(writer http.ResponseWriter, request *http.Request) {
		runtime.GC()
		writer.WriteHeader(http.StatusOK)
	})

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
		Source string `json:"source"` //SourceId
		Setup  string `json:"setup"`  //active/passive
		SSRC   uint32 `json:"ssrc,omitempty"`
	}{}

	//返回监听的端口
	response := &struct {
		IP   string `json:"ip"`
		Port int    `json:"port,omitempty"`
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
		err = &MalformedRequest{Code: http.StatusBadRequest, Msg: fmt.Sprintf("创建GB28181 Source失败 %s 已经存在", v.Source)}
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
			err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "创建GB28181 Source失败, 单端口模式下不能主动拉流"}
		} else if !tcp {
			err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "创建GB28181 Source失败, UDP不能主动拉流"}
		} else if !stream.AppConfig.GB28181.IsEnableTCP() {
			err = &MalformedRequest{Code: http.StatusBadRequest, Msg: "创建GB28181 Source失败, 未开启TCP, UDP不能主动拉流"}
		}

		if err != nil {
			return
		}
	}

	_, port, err := gb28181.NewGBSource(v.Source, v.SSRC, tcp, active)
	if err != nil {
		err = &MalformedRequest{Code: http.StatusInternalServerError, Msg: fmt.Sprintf("创建GB28181 Source失败 err:%s", err.Error())}
		return
	}

	response.IP = stream.AppConfig.PublicIP
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
	sink.SetUrlValues(r.URL.Query())
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
	sink.SetUrlValues(r.URL.Query())
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

	sid := r.URL.Query().Get(hls.SessionIdKey)
	var sink stream.Sink
	if sid != "" {
		sink = stream.SinkManager.Find(stream.SinkId(sid))
	}
	if sink == nil {
		log.Sugar.Errorf("hls session with id '%s' has expired.", sid)
		w.WriteHeader(http.StatusForbidden)
		return
	}

	index := strings.LastIndex(source, "_")
	if index < 0 || index == len(source)-1 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	seq := source[index+1:]
	tsPath := stream.AppConfig.Hls.TSPath(sink.SourceId(), seq)
	if _, err := os.Stat(tsPath); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sink.(*hls.M3U8Sink).RefreshPlayTime()
	w.Header().Set("Content-Type", "video/MP2T")
	http.ServeFile(w, r, tsPath)
}

func (api *ApiServer) onHLS(sourceId string, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("请求m3u8")
	if !stream.AppConfig.Hls.Enable {
		log.Sugar.Warnf("处理hls请求失败 server未开启hls")
		http.Error(w, "hls disable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	//hls_sid是流媒体服务器让播放端, 携带的会话id, 如果没有携带说明是第一次请求播放.
	//播放端不要使用hls_sid这个key, 否则会一直拉流失败
	sid := r.URL.Query().Get(hls.SessionIdKey)
	if sid == "" {
		//让播放端携带会话id
		sid = utils.RandStringBytes(10)

		query := r.URL.Query()
		query.Add(hls.SessionIdKey, sid)
		path := fmt.Sprintf("/%s.m3u8?%s", sourceId, query.Encode())

		response := "#EXTM3U\r\n" +
			"#EXT-X-STREAM-INF:BANDWIDTH=1,AVERAGE-BANDWIDTH=1\r\n" +
			path + "\r\n"
		w.Write([]byte(response))
		return
	}

	sink := stream.SinkManager.Find(sid)
	if sink != nil {
		w.Write([]byte(sink.(*hls.M3U8Sink).GetM3U8String()))
		return
	}

	context := r.Context()
	done := make(chan int, 0)
	sink = hls.NewM3U8Sink(sid, sourceId, func(m3u8 []byte) {
		w.Write(m3u8)
		done <- 0
	}, sid)

	sink.SetUrlValues(r.URL.Query())
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

	sink.SetUrlValues(r.URL.Query())
	log.Sugar.Infof("rtc 请求 sink:%s sdp:%v", sink.PrintInfo(), v.SDP)

	_, state := stream.PreparePlaySink(sink)
	if utils.HookStateOK != state {
		log.Sugar.Warnf("rtc 播放失败 sink:%s", sink.PrintInfo())

		w.WriteHeader(http.StatusForbidden)
		group.Done()
	}

	group.Wait()
}
