package main

import (
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/flv"
	"github.com/yangjiechina/live-server/hls"
	"github.com/yangjiechina/live-server/log"
	"github.com/yangjiechina/live-server/rtc"
	"github.com/yangjiechina/live-server/stream"
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

func startApiServer(addr string) {
	apiServer.router.HandleFunc("/live/{source}", apiServer.filterLive)
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

func (api *ApiServer) generateSinkId(remoteAddr string) stream.SinkId {
	tcpAddr, err := net.ResolveTCPAddr("tcp", remoteAddr)
	if err != nil {
		panic(err)
	}

	return stream.GenerateSinkId(tcpAddr)
}

func (api *ApiServer) doPlay(sink stream.ISink) utils.HookState {
	ok := utils.HookStateOK
	sink.Play(func() {

	}, func(state utils.HookState) {
		ok = state
	})

	return ok
}

func (api *ApiServer) filterLive(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	source := vars["source"]
	index := strings.LastIndex(source, ".")
	if index < 0 || index == len(source)-1 {
		log.Sugar.Errorf("bad request:%s. stream format must be passed at the end of the URL", r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	sourceId := source[:index]
	format := source[index+1:]

	/**
	  http://host:port/xxx.flv
	  http://host:port/xxx.rtc
	  http://host:port/xxx.m3u8
	  http://host:port/xxx_0.ts
	  ws://host:port/xxx.flv
	*/
	if "flv" == format {
		//判断是否是websocket请求
		ws := true
		if !("upgrade" == strings.ToLower(r.Header.Get("Connection"))) {
			ws = false
		} else if !("websocket" == strings.ToLower(r.Header.Get("Upgrade"))) {
			ws = false
		} else if !("13" == r.Header.Get("Sec-Websocket-Version")) {
			ws = false
		}

		if ws {
			api.onWSFlv(sourceId, w, r)
		} else {
			api.onFLV(sourceId, w, r)
		}

	} else if "m3u8" == format {
		api.onHLS(sourceId, w, r)
	} else if "ts" == format {
		api.onTS(sourceId, w, r)
	} else if "rtc" == format {
		api.onRtc(sourceId, w, r)
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

	state := api.doPlay(sink)
	if utils.HookStateOK != state {
		log.Sugar.Warnf("ws-flv 播放失败 state:%d sink:%s", state, sink.PrintInfo())
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

func (api *ApiServer) onFLV(sourceId string, w http.ResponseWriter, r *http.Request) {
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

	state := api.doPlay(sink)
	if utils.HookStateOK != state {
		log.Sugar.Warnf("http-flv 播放失败 state:%d sink:%s", state, sink.PrintInfo())

		w.WriteHeader(http.StatusForbidden)
		return
	}

	bytes := make([]byte, 64)
	for {
		if _, err := conn.Read(bytes); err != nil {
			log.Sugar.Infof("http-flv 断开连接 sink:%s", sink.PrintInfo())
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

		state := api.doPlay(sink)
		if utils.HookStateOK != state {
			log.Sugar.Warnf("m3u8 请求失败 state:%d sink:%s", state, sink.PrintInfo())

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

	state := api.doPlay(sink)
	if utils.HookStateOK != state {
		log.Sugar.Warnf("rtc 播放失败 state:%d sink:%s", state, sink.PrintInfo())

		w.WriteHeader(http.StatusForbidden)
		group.Done()
	}

	group.Wait()
}
