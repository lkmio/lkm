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
		source, err := stream.Path2SourceID(req.URL.Path, suffix)
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
			httpResponseError(w, err.Error())
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
	apiServer.router.HandleFunc("/api/v1/sink/add", filterRequestBodyParams(apiServer.OnSinkAdd, &GBOffer{}))     // 级联/广播/JT转GB

	apiServer.router.HandleFunc("/api/v1/streams/statistics", nil) // 统计所有推拉流

	if stream.AppConfig.GB28181.Enable {
		apiServer.router.HandleFunc("/ws/v1/gb28181/talk", apiServer.OnGBTalk) // 对讲的主讲人WebSocket连接
		apiServer.router.HandleFunc("/api/v1/gb28181/source/create", filterRequestBodyParams(apiServer.OnGBOfferCreate, &SourceSDP{}))
		apiServer.router.HandleFunc("/api/v1/gb28181/answer/set", filterRequestBodyParams(apiServer.OnGBSourceConnect, &SourceSDP{})) // active拉流模式下, 设置对方的地址
	}

	apiServer.router.HandleFunc("/api/v1/gc/force", func(writer http.ResponseWriter, request *http.Request) {
		runtime.GC()
	})

	apiServer.router.PathPrefix("/web/").Handler(http.StripPrefix("/web/", http.FileServer(http.Dir("./web"))))

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

func (api *ApiServer) generateSinkID(remoteAddr string) stream.SinkID {
	tcpAddr, err := net.ResolveTCPAddr("tcp", remoteAddr)
	if err != nil {
		panic(err)
	}

	return stream.NetAddr2SinkID(tcpAddr)
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
		log.Sugar.Errorf("ws拉流失败 source: %s err: %s", sourceId, err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	sink := flv.NewFLVSink(api.generateSinkID(r.RemoteAddr), sourceId, flv.NewWSConn(conn))
	ok := stream.SubscribeStream(sink, r.URL.Query())
	if utils.HookStateOK != ok {
		log.Sugar.Warnf("ws-flv 拉流失败 source: %s sink: %s", sourceId, sink.String())
		_ = conn.Close()
	} else {
		log.Sugar.Infof("ws-flv 拉流成功 source: %s sink: %s", sourceId, sink.String())
	}

	netConn := conn.NetConn()
	bytes := make([]byte, 64)
	for {
		if _, err := netConn.Read(bytes); err != nil {
			log.Sugar.Infof("ws-flv 断开连接 source: %s sink:%s", sourceId, sink.String())
			sink.Close()
			break
		}
	}
}

func (api *ApiServer) onHttpFLV(sourceId string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Connection", "Keep-Alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	var conn net.Conn
	if hj, ok := w.(http.Hijacker); !ok {
		log.Sugar.Errorf("http-flv 拉流失败 不支持hijacking. source: %s remote: %s", sourceId, r.RemoteAddr)
		http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
		return
	} else {
		w.WriteHeader(http.StatusOK)
		var err error
		if conn, _, err = hj.Hijack(); err != nil {
			log.Sugar.Errorf("http-flv 拉流失败 source: %s remote: %s err: %s", sourceId, r.RemoteAddr, err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	sink := flv.NewFLVSink(api.generateSinkID(r.RemoteAddr), sourceId, conn)
	ok := stream.SubscribeStream(sink, r.URL.Query())
	if utils.HookStateOK != ok {
		log.Sugar.Warnf("http-flv 拉流失败 source: %s sink: %s", sourceId, sink.String())
		sink.Close()
	} else {
		log.Sugar.Infof("http-flv 拉流成功 source: %s sink: %s", sourceId, sink.String())
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
	sid := r.URL.Query().Get(hls.SessionIDKey)
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

func (api *ApiServer) onHLS(source string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")

	// 如果没有携带会话ID, 认为是首次拉流. Server将生成会话ID, 应答给拉流端, 后续拉流请求(.M3U8和.TS的HTTP请求)都将携带该会话ID.
	// 会话ID的Key为"hls_sid", 为避免冲突, 播放端和hook server不要再使用, 否则会一直拉流失败.
	sid := r.URL.Query().Get(hls.SessionIDKey)
	if sid == "" {
		sid = utils.RandStringBytes(10)

		query := r.URL.Query()
		query.Add(hls.SessionIDKey, sid)
		path := fmt.Sprintf("/%s.m3u8?%s", source, query.Encode())

		response := "#EXTM3U\r\n" +
			"#EXT-X-STREAM-INF:BANDWIDTH=1,AVERAGE-BANDWIDTH=1\r\n" +
			path + "\r\n"
		w.Write([]byte(response))
		return
	}

	sink := stream.SinkManager.Find(sid)
	// 更新最近的M3U8文件
	if sink != nil {
		w.Write([]byte(sink.(*hls.M3U8Sink).GetPlaylist()))
		return
	}

	// 首次拉流
	context := r.Context()
	m3u8Pipe := make(chan []byte, 1)
	sink = hls.NewM3U8Sink(sid, source, func(m3u8 []byte) {
		m3u8Pipe <- m3u8
	}, sid)

	ok := stream.SubscribeStream(sink, r.URL.Query())
	if utils.HookStateOK != ok {
		log.Sugar.Warnf("m3u8拉流失败 source: %s sink: %s", source, sink.String())
		w.WriteHeader(http.StatusForbidden)
		return
	}

	err := stream.SinkManager.Add(sink)
	utils.Assert(err == nil)

	select {
	case m3u8 := <-m3u8Pipe:
		// 应答M3U8文件
		if m3u8 == nil {
			log.Sugar.Warnf("hls拉流失败 未能生成有效m3u8文件 sink: %s source: %s", sink.GetID(), sink.GetSourceID())
			w.WriteHeader(http.StatusInternalServerError)
			sink.Close()
		} else {
			w.Write(m3u8)
		}
		break
	case <-context.Done():
		// 拉流端断开拉流
		log.Sugar.Infof(stream.CreateSinkDisconnectionMessage(sink))
		sink.Close()
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
		log.Sugar.Errorf("rtc拉流失败 err: %s remote: %s", err.Error(), r.RemoteAddr)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if err := json.Unmarshal(data, &v); err != nil {
		log.Sugar.Errorf("rtc拉流失败 err: %s remote: %s", err.Error(), r.RemoteAddr)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	group := sync.WaitGroup{}
	group.Add(1)
	sink := rtc.NewSink(api.generateSinkID(r.RemoteAddr), sourceId, v.SDP, func(sdp string) {
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

	log.Sugar.Infof("rtc拉流请求 source: %s sink: %s sdp:%v", sourceId, sink.String(), v.SDP)

	ok := stream.SubscribeStream(sink, r.URL.Query())
	if utils.HookStateOK != ok {
		log.Sugar.Warnf("rtc拉流失败 source: %s sink: %s", sourceId, sink.String())
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
		Urls      []string  `json:"urls"`       // 拉流地址
	}

	var details []SourceDetails
	for _, source := range sources {
		var codecs []string
		tracks := source.OriginTracks()
		for _, track := range tracks {
			codecs = append(codecs, track.Stream.CodecID.String())
		}

		details = append(details, SourceDetails{
			ID:        source.GetID(),
			Protocol:  source.GetType().String(),
			Time:      source.CreateTime(),
			SinkCount: source.GetTransStreamPublisher().SinkCount(),
			Bitrate:   strconv.Itoa(source.GetBitrateStatistics().PreviousSecond()/1024) + "KBS", // 后续开发
			Tracks:    codecs,
			Urls:      stream.GetStreamPlayUrls(source.GetID()),
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
	sinks := source.GetTransStreamPublisher().Sinks()
	for _, sink := range sinks {
		details = append(details,
			SinkDetails{
				ID:       stream.SinkID2String(sink.GetID()),
				Protocol: sink.GetProtocol().String(),
				Time:     sink.CreateTime(),
			},
		)
	}

	httpResponseOK(w, details)
}

func (api *ApiServer) OnSourceClose(v *IDS, w http.ResponseWriter, r *http.Request) {
	log.Sugar.Infof("close source: %v", v.Source)

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
		if sink := source.GetTransStreamPublisher().FindSink(sinkId); sink != nil {
			sink.Close()
		}
	} else {
		log.Sugar.Warnf("Source with ID %s does not exist.", v.Source)
	}

	httpResponseOK(w, nil)
}
