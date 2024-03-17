package rtsp

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/stream"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	MethodOptions      = "OPTIONS"
	MethodDescribe     = "DESCRIBE"
	MethodSetup        = "SETUP"
	MethodPlay         = "PLAY"
	MethodTeardown     = "TEARDOWN"
	MethodPause        = "PAUSE"
	MethodGetParameter = "GET_PARAMETER"
	MethodSetParameter = "SET_PARAMETER"

	MethodRedirect = "REDIRECT"
	MethodRecord   = "RECORD"

	Version = "RTSP/1.0"
)

type requestHandler interface {
	onOptions(sourceId string, headers textproto.MIMEHeader)

	onDescribe(sourceId string, headers textproto.MIMEHeader)

	onSetup(sourceId string, index int, headers textproto.MIMEHeader)

	onPlay(sourceId string)

	onTeardown()

	onPause()
}

type session struct {
	conn net.Conn

	sink_       *sink
	sessionId   string
	writeBuffer *bytes.Buffer
}

func NewSession(conn net.Conn) *session {
	milli := int(time.Now().UnixMilli() & 0xFFFFFFFF)
	return &session{
		conn:        conn,
		sessionId:   strconv.Itoa(milli),
		writeBuffer: bytes.NewBuffer(make([]byte, 0, 1024*10)),
	}
}

func NewOKResponse(cseq string) http.Response {
	rep := http.Response{
		Proto:      Version,
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     make(http.Header),
	}
	if cseq == "" {
		cseq = "1"
	}

	rep.Header.Set("Cseq", cseq)
	return rep
}

func parseMessage(data []byte) (string, *url.URL, textproto.MIMEHeader, error) {
	reader := bufio.NewReader(bytes.NewReader(data))
	tp := textproto.NewReader(reader)
	line, err := tp.ReadLine()
	split := strings.Split(line, " ")
	if len(split) < 3 {
		panic(fmt.Errorf("unknow response line of response:%s", line))
	}

	method := strings.ToUpper(split[0])
	//version
	_ = split[2]

	url_, err := url.Parse(split[1])
	if err != nil {
		return "", nil, nil, err
	}

	header, err := tp.ReadMIMEHeader()
	if err != nil {
		return "", nil, nil, err
	}

	return method, url_, header, nil
}

func (s *session) response(response http.Response, body []byte) error {

	//添加Content-Length
	if body != nil {
		response.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}

	// 将响应头和正文封装成字符串
	s.writeBuffer.Reset()
	_, err := fmt.Fprintf(s.writeBuffer, "%s %d %s\r\n", response.Proto, response.StatusCode, response.Status)
	if err != nil {
		return err
	}

	for k, v := range response.Header {
		for _, hv := range v {
			s.writeBuffer.WriteString(fmt.Sprintf("%s: %s\r\n", k, hv))
		}
	}

	//分隔头部与主体
	s.writeBuffer.WriteString("\r\n")
	if body != nil {
		s.writeBuffer.Write(body)
		if body[len(body)-2] != 0x0D || body[len(body)-1] != 0x0A {
			s.writeBuffer.WriteString("\r\n")
		}
	}

	data := s.writeBuffer.Bytes()
	_, err = s.conn.Write(data)
	return err
}

func (s *session) onOptions(sourceId string, headers textproto.MIMEHeader) error {
	rep := NewOKResponse(headers.Get("Cseq"))
	rep.Header.Set("Public", "OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN, PAUSE, GET_PARAMETER, SET_PARAMETER, REDIRECT, RECORD")
	return s.response(rep, nil)
}

func (s *session) onDescribe(source string, headers textproto.MIMEHeader) error {
	var err error
	sinkId := stream.GenerateSinkId(s.conn.RemoteAddr())
	sink_ := NewSink(sinkId, source, s.conn, func(sdp string) {
		response := NewOKResponse(headers.Get("Cseq"))
		response.Header.Set("Content-Type", "application/sdp")
		err = s.response(response, []byte(sdp))
	})

	code := utils.HookStateOK
	s.sink_ = sink_.(*sink)
	sink_.(*sink).Play(sink_, func() {

	}, func(state utils.HookState) {
		code = state
	})

	if utils.HookStateOK != code {
		return fmt.Errorf("hook failed. code:%d", code)
	}

	return err
}

func (s *session) onSetup(sourceId string, index int, headers textproto.MIMEHeader) error {
	transportHeader := headers.Get("Transport")
	if transportHeader == "" {
		return fmt.Errorf("not find transport header")
	}

	split := strings.Split(transportHeader, ";")
	if len(split) < 3 {
		return fmt.Errorf("failed to parsing TRANSPORT header:%s", split)
	}

	var clientRtpPort int
	var clientRtcpPort int
	tcp := "RTP/AVP" != split[0] && "RTP/AVP/UDP" != split[0]
	for _, value := range split {
		if !strings.HasPrefix(value, "client_port=") {
			continue
		}

		pairPort := strings.Split(value[len("client_port="):], "-")
		if len(pairPort) != 2 {
			return fmt.Errorf("failed to parsing client_port:%s", value)
		}

		port, err := strconv.Atoi(pairPort[0])
		if err != nil {
			return err
		}
		clientRtpPort = port

		port, err = strconv.Atoi(pairPort[1])
		if err != nil {
			return err
		}
		clientRtcpPort = port
	}

	rtpPort, rtcpPort, err := s.sink_.addTrack(index, tcp)
	if err != nil {
		return err
	}

	println(clientRtpPort)
	println(clientRtcpPort)
	responseHeader := transportHeader + ";server_port=" + fmt.Sprintf("%d-%d", rtpPort, rtcpPort) + ";ssrc=FFFFFFFF"
	response := NewOKResponse(headers.Get("Cseq"))
	response.Header.Set("Transport", responseHeader)
	response.Header.Set("Session", s.sessionId)

	return s.response(response, nil)
}

func (s *session) onPlay(sourceId string, headers textproto.MIMEHeader) error {
	response := NewOKResponse(headers.Get("Cseq"))
	sessionHeader := headers.Get("Session")
	if sessionHeader != "" {
		response.Header.Set("Session", sessionHeader)
	}

	return s.response(response, nil)
}

func (s *session) onTeardown() {
}

func (s *session) onPause() {

}

func (s *session) Input(method string, url_ *url.URL, headers textproto.MIMEHeader) error {
	//_ = url_.User.Username()
	//_, _ = url_.User.Password()

	var err error
	split := strings.Split(url_.Path, "/")
	source := split[len(split)-1]
	if MethodOptions == method {
		err = s.onOptions(source, headers)
	} else if MethodDescribe == method {
		err = s.onDescribe(source, headers)
	} else if MethodSetup == method {
		query, err := url.ParseQuery(url_.RawQuery)
		if err != nil {
			return err
		}

		track := query.Get("track")
		index, err := strconv.Atoi(track)
		if err != nil {
			return err
		}

		if err = s.onSetup(source, index, headers); err != nil {
			return err
		}
	} else if MethodPlay == method {
		err = s.onPlay(source, headers)
	} else if MethodTeardown == method {
		s.onTeardown()
	} else if MethodPause == method {
		s.onPause()
	}

	return err
}

func (s *session) close() {

}
