package main

import (
	"context"
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/flv"
	"github.com/yangjiechina/live-server/rtc"
	"github.com/yangjiechina/live-server/stream"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func startApiServer(addr string) {
	r := mux.NewRouter()
	r.HandleFunc("/live/flv/{source}", onFLV)
	r.HandleFunc("/live/hls/{source}", onHLS)
	r.HandleFunc("/live/rtc/{source}", onRtc)
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

func onFLV(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	source := vars["source"]

	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
		return
	}
	context_ := r.Context()
	w.WriteHeader(http.StatusOK)

	conn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var sourceId string
	if index := strings.LastIndex(source, "."); index > -1 {
		sourceId = source[:index]
	}

	tcpAddr, _ := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	sinkId := stream.GenerateSinkId(tcpAddr)
	sink := flv.NewFLVSink(sinkId, sourceId, conn)

	go func(ctx context.Context) {
		sink.(*stream.SinkImpl).Play(sink, func() {
			//sink.(*stream.SinkImpl).PlayDone(sink, nil, nil)
		}, func(state utils.HookState) {
			conn.Close()
		})
	}(context_)
}

func onHLS(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	source := vars["source"]

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")

	//删除末尾的.ts/.m3u8, 请确保id中不存在.
	//var sourceId string
	//if index := strings.LastIndex(source, "."); index > -1 {
	//	sourceId = source[:index]
	//}
	//
	//tcpAddr, _ := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	//sinkId := stream.GenerateSinkId(tcpAddr)
	if strings.HasSuffix(source, ".m3u8") {
		//查询是否存在hls流, 不存在-等生成后再响应m3u8文件. 存在-直接响应m3u8文件
		http.ServeFile(w, r, "../tmp/"+source)
	} else if strings.HasSuffix(source, ".ts") {
		http.ServeFile(w, r, "../tmp/"+source)
	}

}

func onRtc(w http.ResponseWriter, r *http.Request) {
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

	sinkId := stream.SinkId(123)
	split := strings.Split(r.URL.Path, "/")

	group := sync.WaitGroup{}
	group.Add(1)
	sink := rtc.NewSink(sinkId, split[len(split)-1], v.SDP, func(sdp string) {
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
		group.Done()
	})
	group.Wait()
}
