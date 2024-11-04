package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/lkm/flv"
	"github.com/lkmio/lkm/hls"
	"github.com/lkmio/lkm/log"
	"github.com/lkmio/lkm/rtc"
	"github.com/lkmio/lkm/stream"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
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

func filterSourceID(f func(sourceId string, w http.ResponseWriter, req *http.Request), suffix string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		source, err := stream.Path2SourceId(req.URL.Path, suffix)
		if err != nil {
			log.Sugar.Errorf("拉流失败 解析流id发生err: %s path: %s", err.Error(), req.URL.Path)
			httpResponse(w, http.StatusBadRequest, err.Error())
			return
		}

		f(source, w, req)
	}
}

type IDS struct {
	// 内部SinkID可能是uint64或者string类型, 但外部传参均使用string类型，程序内部自行兼容ipv6.
	Sink   string `json:"sink"`
	Source string `json:"source"`
}

func filterRequestBodyParams[T any](f func(params T, w http.ResponseWriter, req *http.Request), params interface{}) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if err := HttpDecodeJSONBody(w, req, params); err != nil {
			log.Sugar.Errorf("处理http请求失败 err: %s path: %s", err.Error(), req.URL.Path)
			httpResponse2(w, err)
			return
		}

		f(params.(T), w, req)
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
	// {source}.flv和/{source}/{stream}.flv意味着, 推流id(路径)只能嵌套一层
	apiServer.router.HandleFunc("/{source}.flv", filterSourceID(apiServer.onFlv, ".flv"))
	apiServer.router.HandleFunc("/{source}/{stream}.flv", filterSourceID(apiServer.onFlv, ".flv"))

	if stream.AppConfig.Hls.Enable {
		apiServer.router.HandleFunc("/{source}.m3u8", filterSourceID(apiServer.onHLS, ".m3u8"))
		apiServer.router.HandleFunc("/{source}/{stream}.m3u8", filterSourceID(apiServer.onHLS, ".m3u8"))
		apiServer.router.HandleFunc("/{source}.ts", filterSourceID(apiServer.onTS, ".ts"))
		apiServer.router.HandleFunc("/{source}/{stream}.ts", filterSourceID(apiServer.onTS, ".ts"))
	}

	if stream.AppConfig.WebRtc.Enable {
		apiServer.router.HandleFunc("/{source}.rtc", filterSourceID(apiServer.onRtc, ".rtc"))
		apiServer.router.HandleFunc("/{source}/{stream}.rtc", filterSourceID(apiServer.onRtc, ".rtc"))
	}

	apiServer.router.HandleFunc("/api/v1/source/list", apiServer.OnSourceList)                                    // 查询所有推流源
	apiServer.router.HandleFunc("/api/v1/source/close", filterRequestBodyParams(apiServer.OnSourceClose, &IDS{})) // 关闭推流源
	apiServer.router.HandleFunc("/api/v1/sink/list", filterRequestBodyParams(apiServer.OnSinkList, &IDS{}))       // 查询某个推流源下，所有的拉流端列表
	apiServer.router.HandleFunc("/api/v1/sink/close", filterRequestBodyParams(apiServer.OnSinkClose, &IDS{}))     // 关闭拉流端

	apiServer.router.HandleFunc("/api/v1/streams/statistics", nil) // 统计所有推拉流

	apiServer.router.HandleFunc("/api/v1/gb28181/forward", filterRequestBodyParams(apiServer.OnGBSourceForward, &GBForwardParams{}))     // 设置级联转发目标，停止级联调用sink/close接口，级联断开会走on_play_done事件通知
	apiServer.router.HandleFunc("/api/v1/gb28181/source/create", filterRequestBodyParams(apiServer.OnGBSourceCreate, &GBSourceParams{})) // 创建国标推流源
	apiServer.router.HandleFunc("/api/v1/gb28181/source/connect", filterRequestBodyParams(apiServer.OnGBSourceConnect, &GBConnect{}))    // 为国标TCP主动推流，设置连接地址

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

func (api *ApiServer) generateSinkId(remoteAddr string) stream.SinkID {
	tcpAddr, err := net.ResolveTCPAddr("tcp", remoteAddr)
	if err != nil {
		panic(err)
	}

	return stream.NetAddr2SinkId(tcpAddr)
}

func (api *ApiServer) onFlv(sourceId string, w http.ResponseWriter, r *http.Request) {
	// 区分ws请求
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
	log.Sugar.Infof("ws-flv 连接 sink:%s", sink.String())

	_, state := stream.PreparePlaySink(sink)
	if utils.HookStateOK != state {
		log.Sugar.Warnf("ws-flv 播放失败 sink:%s", sink.String())
		w.WriteHeader(http.StatusForbidden)
		return
	}

	netConn := conn.NetConn()
	bytes := make([]byte, 64)
	for {
		if _, err := netConn.Read(bytes); err != nil {
			log.Sugar.Infof("ws-flv 断开连接 sink:%s", sink.String())
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
	log.Sugar.Infof("http-flv 连接 sink:%s", sink.String())

	_, state := stream.PreparePlaySink(sink)
	if utils.HookStateOK != state {
		log.Sugar.Warnf("http-flv 播放失败 sink:%s", sink.String())

		w.WriteHeader(http.StatusForbidden)
		return
	}

	bytes := make([]byte, 64)
	for {
		if _, err := conn.Read(bytes); err != nil {
			log.Sugar.Infof("http-flv 断开连接 sink:%s", sink.String())
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
		sink = stream.SinkManager.Find(stream.SinkID(sid))
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
	tsPath := stream.AppConfig.Hls.TSPath(sink.GetSourceID(), seq)
	if _, err := os.Stat(tsPath); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	sink.(*hls.M3U8Sink).RefreshPlayTime()
	w.Header().Set("Content-Type", "video/MP2T")
	http.ServeFile(w, r, tsPath)
}

func (api *ApiServer) onHLS(sourceId string, w http.ResponseWriter, r *http.Request) {
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
		log.Sugar.Warnf("m3u8 请求失败 sink:%s", sink.String())

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
	log.Sugar.Infof("rtc 请求 sink:%s sdp:%v", sink.String(), v.SDP)

	_, state := stream.PreparePlaySink(sink)
	if utils.HookStateOK != state {
		log.Sugar.Warnf("rtc 播放失败 sink:%s", sink.String())

		w.WriteHeader(http.StatusForbidden)
		group.Done()
	}

	group.Wait()
}

func (api *ApiServer) OnSourceList(w http.ResponseWriter, r *http.Request) {
	sources := stream.SourceManager.All()

	type SourceDetails struct {
		ID        string    `json:"id"`
		Protocol  string    `json:"protocol"`   // 推流协议
		Time      time.Time `json:"time"`       // 推流时间
		SinkCount int       `json:"sink_count"` // 播放端计数
		Bitrate   string    `json:"bitrate"`    // 码率统计
		Tracks    []string  `json:"tracks"`     // 每路流编码器ID
	}

	var details []SourceDetails
	for _, source := range sources {
		var tracks []string
		streams := source.OriginStreams()
		for _, avStream := range streams {
			tracks = append(tracks, avStream.CodecId().String())
		}

		details = append(details, SourceDetails{
			ID:        source.GetID(),
			Protocol:  source.GetType().String(),
			Time:      source.CreateTime(),
			SinkCount: source.SinkCount(),
			Bitrate:   "", // 后续开发
			Tracks:    tracks,
		})
	}

	httpResponseOK(w, details)
}

func (api *ApiServer) OnSinkList(v *IDS, w http.ResponseWriter, r *http.Request) {
	source := stream.SourceManager.Find(v.Source)
	if source == nil {
		httpResponseOK(w, nil)
		return
	}

	type SinkDetails struct {
		ID       string    `json:"id"`
		Protocol string    `json:"protocol"` // 拉流协议
		Time     time.Time `json:"time"`     // 拉流时间
		Bitrate  string    `json:"bitrate"`  // 码率统计
		Tracks   []string  `json:"tracks"`   // 每路流编码器ID
	}

	var details []SinkDetails
	sinks := source.Sinks()
	for _, sink := range sinks {
		details = append(details,
			SinkDetails{
				ID:       stream.SinkId2String(sink.GetID()),
				Protocol: sink.GetProtocol().String(),
				Time:     sink.CreateTime(),
			},
		)
	}

	httpResponseOK(w, details)
}

func (api *ApiServer) OnSourceClose(v *IDS, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("close source: %v", v)

	if source := stream.SourceManager.Find(v.Source); source != nil {
		source.Close()
	} else {
		log.Sugar.Warnf("Source with ID %s does not exist.", v.Source)
	}

	httpResponseOK(w, nil)
}

func (api *ApiServer) OnSinkClose(v *IDS, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("close sink: %v", v)

	var sinkId stream.SinkID
	i, err := strconv.ParseUint(v.Sink, 10, 64)
	if err != nil {
		sinkId = stream.SinkID(v.Sink)
	} else {
		sinkId = stream.SinkID(i)
	}

	if source := stream.SourceManager.Find(v.Source); source != nil {
		if sink := source.FindSink(sinkId); sink != nil {
			sink.Close()
		}
	} else {
		log.Sugar.Warnf("Source with ID %s does not exist.", v.Source)
	}

	httpResponseOK(w, nil)
}
