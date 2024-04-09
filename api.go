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

var upgrader *websocket.Upgrader

func init() {
	upgrader = &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
}

func startApiServer(addr string) {
	r := mux.NewRouter()
	/**
	  http://host:port/xxx.flv
	  http://host:port/xxx.rtc
	  http://host:port/xxx.m3u8
	  http://host:port/xxx_0.ts
	  ws://host:port/xxx.flv
	*/
	r.HandleFunc("/live/{source}", func(w http.ResponseWriter, r *http.Request) {
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
				onWSFlv(sourceId, w, r)
			} else {
				onFLV(sourceId, w, r)
			}

		} else if "m3u8" == format {
			onHLS(sourceId, w, r)
		} else if "ts" == format {
			onTS(sourceId, w, r)
		} else if "rtc" == format {
			onRtc(sourceId, w, r)
		}
	})

	r.HandleFunc("/rtc.html", func(writer http.ResponseWriter, request *http.Request) {
		http.ServeFile(writer, request, "./rtc.html")
	})
	http.Handle("/", r)

	srv := &http.Server{
		Handler: r,
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

func onWSFlv(sourceId string, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Sugar.Errorf("websocket头检查失败 err:%s", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	tcpAddr, _ := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	sinkId := stream.GenerateSinkId(tcpAddr)
	sink := flv.NewFLVSink(sinkId, sourceId, flv.NewWSConn(conn))

	log.Sugar.Infof("ws-flv 连接 sink:%s", sink.PrintInfo())

	sink.(*stream.SinkImpl).Play(sink, func() {

	}, func(state utils.HookState) {
		w.WriteHeader(http.StatusForbidden)

		conn.Close()
	})

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

func onFLV(sourceId string, w http.ResponseWriter, r *http.Request) {
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

	tcpAddr, _ := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	sinkId := stream.GenerateSinkId(tcpAddr)
	sink := flv.NewFLVSink(sinkId, sourceId, conn)

	log.Sugar.Infof("http-flv 连接 sink:%s", sink.PrintInfo())
	sink.(*stream.SinkImpl).Play(sink, func() {

	}, func(state utils.HookState) {
		w.WriteHeader(http.StatusForbidden)

		conn.Close()
	})

	bytes := make([]byte, 64)
	for {
		if _, err := conn.Read(bytes); err != nil {
			log.Sugar.Infof("http-flv 断开连接 sink:%s", sink.PrintInfo())
			break
		}
	}
}

func onTS(source string, w http.ResponseWriter, r *http.Request) {
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

func onHLS(sourceId string, w http.ResponseWriter, r *http.Request) {
	if !stream.AppConfig.Hls.Enable {
		log.Sugar.Warnf("处理hls请求失败 server未开启hls request:%s", r.URL.Path)
		http.Error(w, "hls disable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	//m3u8和ts会一直刷新, 每个请求只hook一次.
	tcpAddr, _ := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	sinkId := stream.GenerateSinkId(tcpAddr)

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

		hookState := utils.HookStateOK
		sink.Play(sink, func() {
			err := stream.SinkManager.Add(sink)

			utils.Assert(err == nil)
		}, func(state utils.HookState) {
			log.Sugar.Warnf("hook播放事件失败 request:%s", r.URL.Path)
			hookState = state
			w.WriteHeader(http.StatusForbidden)
		})

		if utils.HookStateOK != hookState {
			return
		}

		select {
		case <-done:
		case <-context.Done():
			log.Sugar.Infof("http m3u8连接断开")
			break
		}
	}
}

func onRtc(sourceId string, w http.ResponseWriter, r *http.Request) {
	v := struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	}{}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}

	if err := json.Unmarshal(data, &v); err != nil {
		panic(err)
	}

	tcpAddr, _ := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	sinkId := stream.GenerateSinkId(tcpAddr)

	group := sync.WaitGroup{}
	group.Add(1)
	sink := rtc.NewSink(sinkId, sourceId, v.SDP, func(sdp string) {
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

	sink.Play(sink, func() {

	}, func(state utils.HookState) {
		w.WriteHeader(http.StatusForbidden)

		group.Done()
	})

	group.Wait()
}
