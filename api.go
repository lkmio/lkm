package main

import (
	"encoding/base64"
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
	"strconv"
	"strings"
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

func withJsonParams[T any](f func(params T, w http.ResponseWriter, req *http.Request), params interface{}) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		newParams := new(T)
		if err := HttpDecodeJSONBody(w, req, newParams); err != nil {
			log.Sugar.Errorf("处理http请求失败 err: %s path: %s", err.Error(), req.URL.Path)
			httpResponseError(w, err.Error())
			return
		}

		f(*newParams, w, req)
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

	apiServer.router.Use(func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 添加 CORS 头以解决跨域问题
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "*")

			// 如果是OPTIONS请求，直接返回
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			handler.ServeHTTP(w, r)
		})
	})
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

	apiServer.router.HandleFunc("/api/v1/source/list", apiServer.OnSourceList)                           // 查询所有推流源
	apiServer.router.HandleFunc("/api/v1/source/close", withJsonParams(apiServer.OnSourceClose, &IDS{})) // 关闭推流源
	apiServer.router.HandleFunc("/api/v1/sink/list", withJsonParams(apiServer.OnSinkList, &IDS{}))       // 查询某个推流源下，所有的拉流端列表
	apiServer.router.HandleFunc("/api/v1/sink/close", withJsonParams(apiServer.OnSinkClose, &IDS{}))     // 关闭拉流端
	apiServer.router.HandleFunc("/api/v1/sink/add", withJsonParams(apiServer.OnSinkAdd, &GBOffer{}))     // 级联/广播/JT转GB

	apiServer.router.HandleFunc("/api/v1/streams/statistics", nil) // 统计所有推拉流

	if stream.AppConfig.GB28181.Enable {
		apiServer.router.HandleFunc("/ws/v1/gb28181/talk", apiServer.OnGBTalk) // 对讲的主讲人WebSocket连接
		apiServer.router.HandleFunc("/api/v1/gb28181/source/create", withJsonParams(apiServer.OnGBOfferCreate, &SourceSDP{}))
		apiServer.router.HandleFunc("/api/v1/gb28181/answer/set", withJsonParams(apiServer.OnGBSourceConnect, &SourceSDP{})) // active拉流模式下, 设置对方的地址
	}

	apiServer.router.HandleFunc("/api/v1/gc/force", func(writer http.ResponseWriter, request *http.Request) {
		runtime.GC()
	})

	apiServer.router.HandleFunc("/api/v1/stream/info", apiServer.OnStreamInfo)

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

func (api *ApiServer) generateSinkID(_ string) stream.SinkID {
	return utils.RandStringBytes(18)
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
		sink = hls.SinkManager.Find(stream.SinkID(sid))
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

	sink.(*hls.M3U8Sink).RefreshPlayingTime()
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

	sink := hls.SinkManager.Find(sid)
	if sink == nil {
		// 创建sink
		sink = hls.NewM3U8Sink(sid, source, sid)
		sink.(*hls.M3U8Sink).RefreshPlayingTime()

		if hls.SinkManager.Add(sink) {
			ok := stream.SubscribeStream(sink, r.URL.Query())
			if utils.HookStateOK != ok {
				log.Sugar.Warnf("m3u8拉流失败 source: %s sink: %s", source, sink.String())
				_ = hls.SinkManager.Remove(sink.GetID())
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
	}

	// 更新最近的M3U8文件
	playlist := sink.(*hls.M3U8Sink).GetPlaylist(nil)
	if playlist == "" {
		if playlist = sink.(*hls.M3U8Sink).GetPlaylist(r.Context()); playlist == "" {
			log.Sugar.Warnf("hls拉流失败 未能生成有效m3u8文件 sink: %s source: %s", sink.GetID(), sink.GetSourceID())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	w.Write([]byte(playlist))
}

func (api *ApiServer) onRtc(sourceId string, w http.ResponseWriter, r *http.Request) {
	v := struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	}{}

	data, err := io.ReadAll(r.Body)
	var liveGBSWF bool

	if err != nil {
		log.Sugar.Errorf("rtc拉流失败 err: %s remote: %s", err.Error(), r.RemoteAddr)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if liveGBSWF = "livegbs" == r.URL.Query().Get("wf"); liveGBSWF {
		// 兼容livegbs前端播放webrtc
		offer, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			log.Sugar.Errorf("rtc拉流失败 err: %s remote: %s", err.Error(), r.RemoteAddr)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		v.Type = "offer"
		v.SDP = string(offer)
	} else if err := json.Unmarshal(data, &v); err != nil {
		log.Sugar.Errorf("rtc拉流失败 err: %s remote: %s", err.Error(), r.RemoteAddr)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	done := make(chan struct{})
	sink := rtc.NewSink(api.generateSinkID(r.RemoteAddr), sourceId, v.SDP, func(sdp string) {
		response := struct {
			Type string `json:"type"`
			SDP  string `json:"sdp"`
		}{
			Type: "answer",
			SDP:  sdp,
		}

		var body []byte
		body, err = json.Marshal(response)
		if err != nil {
			panic(err)
		}

		if liveGBSWF {
			body = []byte(base64.StdEncoding.EncodeToString([]byte(sdp)))
		} else {
			w.Header().Set("Content-Type", "application/json")
		}

		w.Write(body)
		close(done)
	})

	log.Sugar.Infof("rtc拉流请求 source: %s sink: %s sdp:%v", sourceId, sink.String(), v.SDP)

	ok := stream.SubscribeStream(sink, r.URL.Query())
	if utils.HookStateOK != ok {
		log.Sugar.Warnf("rtc拉流失败 source: %s sink: %s", sourceId, sink.String())
		w.WriteHeader(http.StatusForbidden)
		return
	}

	select {
	case <-r.Context().Done():
		log.Sugar.Infof("rtc拉流请求取消 source: %s sink: %s", sourceId, stream.SinkID2String(sink.GetID()))
		sink.Close()
		break
	case <-done:
		break
	}
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

			if sink.GetProtocol() == stream.TransStreamHls {
				_ = hls.SinkManager.Remove(sinkId)
			}
		}
	} else {
		log.Sugar.Warnf("Source with ID %s does not exist.", v.Source)
	}

	httpResponseOK(w, nil)
}

func (api *ApiServer) OnStreamInfo(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("streamid")
	source := stream.SourceManager.Find(id)
	if source == nil || !source.IsCompleted() || source.IsClosed() {
		return
	}

	tracks := source.OriginTracks()
	if len(tracks) < 1 {
		return
	}

	var deviceId string
	var channelId string
	split := strings.Split(id, "/")
	if len(split) < 2 {
		return
	}

	deviceId = split[0]
	channelId = split[1]
	if len(split[1]) >= 20 {
		channelId = split[1][:20]
	}

	var transport string
	if stream.SourceType28181 == source.GetType() {
		if gb28181.SetupUDP != source.(gb28181.GBSource).SetupType() {
			transport = "TCP"
		} else {
			transport = "UDP"
		}
	}

	var token string
	cookie, err := r.Cookie("token")
	if err == nil {
		token = cookie.Value
	}

	urls := stream.GetStreamPlayUrlsMap(id)
	liveGBSUrls := make(map[string]string)
	for streamName, url := range urls {
		url += "?stream_token=" + token

		// 兼容livegbs前端播放webrtc
		if streamName == "rtc" {
			if strings.HasPrefix(url, "http") {
				url = strings.Replace(url, "http", "webrtc", 1)
			} else if strings.HasPrefix(url, "https") {
				url = strings.Replace(url, "https", "webrtcs", 1)
			}

			url += "&wf=livegbs"
		}

		liveGBSUrls[streamName] = url
	}

	statistics := source.GetBitrateStatistics()
	response := struct {
		AudioEnable           bool   `json:"AudioEnable"`
		CDN                   string `json:"CDN"`
		CascadeSize           int    `json:"CascadeSize"`
		ChannelID             string `json:"ChannelID"`
		ChannelName           string `json:"ChannelName"`
		CloudRecord           bool   `json:"CloudRecord"`
		DecodeSize            int    `json:"DecodeSize"`
		DeviceID              string `json:"DeviceID"`
		Duration              int    `json:"Duration"`
		FLV                   string `json:"FLV"`
		HLS                   string `json:"HLS"`
		InBitRate             int    `json:"InBitRate"`
		InBytes               int    `json:"InBytes"`
		NumOutputs            int    `json:"NumOutputs"`
		Ondemand              bool   `json:"Ondemand"`
		OutBytes              int    `json:"OutBytes"`
		RTMP                  string `json:"RTMP"`
		RTPCount              int    `json:"RTPCount"`
		RTPLostCount          int    `json:"RTPLostCount"`
		RTPLostRate           int    `json:"RTPLostRate"`
		RTSP                  string `json:"RTSP"`
		RecordStartAt         string `json:"RecordStartAt"`
		RelaySize             int    `json:"RelaySize"`
		SMSID                 string `json:"SMSID"`
		SnapURL               string `json:"SnapURL"`
		SourceAudioCodecName  string `json:"SourceAudioCodecName"`
		SourceAudioSampleRate int    `json:"SourceAudioSampleRate"`
		SourceVideoCodecName  string `json:"SourceVideoCodecName"`
		SourceVideoFrameRate  int    `json:"SourceVideoFrameRate"`
		SourceVideoHeight     int    `json:"SourceVideoHeight"`
		SourceVideoWidth      int    `json:"SourceVideoWidth"`
		StartAt               string `json:"StartAt"`
		StreamID              string `json:"StreamID"`
		Transport             string `json:"Transport"`
		VideoFrameCount       int    `json:"VideoFrameCount"`
		WEBRTC                string `json:"WEBRTC"`
		WS_FLV                string `json:"WS_FLV"`
	}{
		AudioEnable:          true,
		CDN:                  "",
		CascadeSize:          0,
		ChannelID:            channelId,
		ChannelName:          "",
		CloudRecord:          false,
		DecodeSize:           0,
		DeviceID:             deviceId,
		Duration:             int(time.Since(source.CreateTime()).Seconds()),
		FLV:                  liveGBSUrls["flv"],
		HLS:                  liveGBSUrls["hls"],
		InBitRate:            statistics.PreviousSecond() * 8 / 1024,
		InBytes:              int(statistics.Total()),
		NumOutputs:           0,
		Ondemand:             true,
		OutBytes:             0,
		RTMP:                 liveGBSUrls["rtmp"],
		RTPCount:             0,
		RTPLostCount:         0,
		RTPLostRate:          0,
		RTSP:                 liveGBSUrls["rtsp"],
		RecordStartAt:        "",
		RelaySize:            0,
		SMSID:                "",
		SnapURL:              "",
		SourceVideoFrameRate: 0,
		StartAt:              source.CreateTime().Format("2006-01-02 15:04:05"),
		StreamID:             id,
		Transport:            transport,
		VideoFrameCount:      0,
		WEBRTC:               liveGBSUrls["rtc"],
		WS_FLV:               liveGBSUrls["ws_flv"],
	}

	for _, track := range tracks {
		if utils.AVMediaTypeAudio == track.Stream.MediaType {
			response.SourceAudioCodecName = track.Stream.CodecID.String()
			response.SourceAudioSampleRate = track.Stream.AudioConfig.SampleRate
		} else if utils.AVMediaTypeVideo == track.Stream.MediaType {
			response.SourceVideoCodecName = track.Stream.CodecID.String()
			// response.SourceVideoFrameRate
			if track.Stream.CodecParameters != nil {
				response.SourceVideoWidth = track.Stream.CodecParameters.Width()
				response.SourceVideoHeight = track.Stream.CodecParameters.Height()
			}
		}
	}

	httpResponseJson(w, &response)
}
