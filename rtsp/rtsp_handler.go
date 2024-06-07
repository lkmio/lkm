package rtsp

import (
	"fmt"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/lkm/log"
	"github.com/yangjiechina/lkm/stream"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

type Request struct {
	session  *session
	sourceId string
	method   string
	url      *url.URL
	headers  textproto.MIMEHeader
}

// Handler 处理RTSP各个请求消息
type Handler interface {
	// Process 路由请求给具体的handler, 并发送响应
	Process(session *session, method string, url_ *url.URL, headers textproto.MIMEHeader) error

	OnOptions(request Request) (*http.Response, []byte, error)

	// OnDescribe 获取spd
	OnDescribe(request Request) (*http.Response, []byte, error)

	// OnSetup 订阅track
	OnSetup(request Request) (*http.Response, []byte, error)

	// OnPlay 请求播放
	OnPlay(request Request) (*http.Response, []byte, error)

	// OnTeardown 结束播放
	OnTeardown(request Request) (*http.Response, []byte, error)

	OnPause(request Request) (*http.Response, []byte, error)

	OnGetParameter(request Request) (*http.Response, []byte, error)

	OnSetParameter(request Request) (*http.Response, []byte, error)

	OnRedirect(request Request) (*http.Response, []byte, error)

	// OnRecord 推流
	OnRecord(request Request) (*http.Response, []byte, error)
}

type handler struct {
	methods      map[string]reflect.Value
	password     string
	publicHeader string
}

func (h handler) Process(session *session, method string, url_ *url.URL, headers textproto.MIMEHeader) error {
	m, ok := h.methods[method]
	if !ok {
		return fmt.Errorf("the method %s is not implmented", method)
	}

	//确保拉流要经过授权
	state, ok := method2StateMap[method]
	if ok && state > SessionStateSetup && session.sink_ == nil {
		return fmt.Errorf("please establish a session first")
	}

	source := strings.TrimSpace(url_.Path)
	if strings.HasPrefix(source, "/") {
		source = source[1:]
	}

	if len(strings.TrimSpace(source)) == 0 {
		return fmt.Errorf("the request source cannot be empty")
	}

	//反射调用各个处理函数
	results := m.Call([]reflect.Value{
		reflect.ValueOf(&h),
		reflect.ValueOf(Request{session, source, method, url_, headers}),
	})

	err, _ := results[2].Interface().(error)
	if err != nil {
		return err
	}

	response := results[0].Interface().(*http.Response)
	if ok {
		session.state = state
	}
	if response == nil {
		return nil
	}

	body := results[1].Bytes()
	err = session.response(response, body)
	return err
}

func (h handler) OnOptions(request Request) (*http.Response, []byte, error) {
	rep := NewOKResponse(request.headers.Get("Cseq"))
	rep.Header.Set("Public", h.publicHeader)
	return rep, nil, nil
}

func (h handler) OnDescribe(request Request) (*http.Response, []byte, error) {
	var err error
	var response *http.Response
	var body []byte

	//校验密码
	if h.password != "" {
		var success bool

		authorization := request.headers.Get("Authorization")
		if authorization != "" {
			params, err := parseAuthParams(authorization)
			success = err == nil && DoAuthenticatePlainTextPassword(params, h.password)
		}

		if !success {
			response401 := NewResponse(http.StatusUnauthorized, request.headers.Get("Cseq"))
			response401.Header.Set("WWW-Authenticate", generateAuthHeader("lkm"))
			return response401, nil, nil
		}
	}

	sinkId := stream.GenerateSinkId(request.session.conn.RemoteAddr())
	sink_ := NewSink(sinkId, request.sourceId, request.session.conn, func(sdp string) {
		response = NewOKResponse(request.headers.Get("Cseq"))
		response.Header.Set("Content-Type", "application/sdp")
		request.session.response(response, []byte(sdp))
	})

	_, code := stream.PreparePlaySink(sink_)
	if utils.HookStateOK != code {
		return nil, nil, fmt.Errorf("hook failed. code:%d", code)
	}

	request.session.sink_ = sink_.(*sink)
	return nil, body, err
}

func (h handler) OnSetup(request Request) (*http.Response, []byte, error) {
	var response *http.Response

	query, err := url.ParseQuery(request.url.RawQuery)
	if err != nil {
		return nil, nil, err
	}

	track := query.Get("track")
	index, err := strconv.Atoi(track)
	if err != nil {
		return nil, nil, err
	}

	transportHeader := request.headers.Get("Transport")
	if transportHeader == "" {
		return nil, nil, fmt.Errorf("not find transport header")
	}

	split := strings.Split(transportHeader, ";")
	if len(split) < 3 {
		return nil, nil, fmt.Errorf("failed to parsing TRANSPORT header:%s", transportHeader)
	}

	tcp := "RTP/AVP" != split[0] && "RTP/AVP/UDP" != split[0]
	if !tcp {
		for _, value := range split {
			if !strings.HasPrefix(value, "client_port=") {
				continue
			}

			pairPort := strings.Split(value[len("client_port="):], "-")
			if len(pairPort) != 2 {
				return nil, nil, fmt.Errorf("failed to parsing client_port:%s", value)
			}

			port, err := strconv.Atoi(pairPort[0])
			if err != nil {
				return nil, nil, err
			}
			_ = port

			port2, err := strconv.Atoi(pairPort[1])
			if err != nil {
				return nil, nil, err
			}
			_ = port2

			log.Sugar.Debugf("client port:%d-%d", port, port2)
		}
	}

	ssrc := 0xFFFFFFFF
	rtpPort, rtcpPort, err := request.session.sink_.addSender(index, tcp, uint32(ssrc))
	if err != nil {
		return nil, nil, err
	}

	responseHeader := transportHeader
	if tcp {
		//修改interleaved为实际的stream index
		responseHeader += ";interleaved=" + fmt.Sprintf("%d-%d", index, index)
	} else {
		responseHeader += ";server_port=" + fmt.Sprintf("%d-%d", rtpPort, rtcpPort)
	}

	responseHeader += ";ssrc=" + strconv.FormatInt(int64(ssrc), 16)

	response = NewOKResponse(request.headers.Get("Cseq"))
	response.Header.Set("Transport", responseHeader)
	response.Header.Set("Session", request.session.sessionId)

	return response, nil, nil
}

func (h handler) OnPlay(request Request) (*http.Response, []byte, error) {
	response := NewOKResponse(request.headers.Get("Cseq"))
	sessionHeader := request.headers.Get("Session")
	if sessionHeader != "" {
		response.Header.Set("Session", sessionHeader)
	}

	request.session.sink_.playing = true
	return response, nil, nil
}

func (h handler) OnTeardown(request Request) (*http.Response, []byte, error) {
	response := NewOKResponse(request.headers.Get("Cseq"))
	return response, nil, nil
}

func (h handler) OnPause(request Request) (*http.Response, []byte, error) {
	response := NewOKResponse(request.headers.Get("Cseq"))
	return response, nil, nil
}

func newHandler(password string) *handler {
	h := handler{
		methods:  make(map[string]reflect.Value, 10),
		password: password,
	}

	//反射获取所有成员函数, 映射对应的RTSP请求方法
	t := reflect.TypeOf(&h)
	numMethod := t.NumMethod()
	headers := make([]string, 0, 10)
	for i := 0; i < numMethod; i++ {
		method := t.Method(i)
		if !strings.HasPrefix(method.Name, "On") {
			continue
		}

		//确保函数名和RTSP标准的请求方法保持一致
		methodName := strings.ToUpper(method.Name[2:])
		h.methods[methodName] = method.Func
		headers = append(headers, methodName)
	}

	h.publicHeader = strings.Join(headers, ",")
	return &h
}
