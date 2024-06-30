package rtsp

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type SessionState int

const (
	Version = "RTSP/1.0"

	SessionStateOptions  = SessionState(0x1)
	SessionStateDescribe = SessionState(0x2)
	SessionStateSetup    = SessionState(0x3)
	SessionStatePlay     = SessionState(0x4)
	SessionStateTeardown = SessionState(0x5)
	SessionStatePause    = SessionState(0x6)
)

var (
	method2StateMap map[string]SessionState
)

func init() {
	method2StateMap = map[string]SessionState{
		"OPTIONS":  SessionStateOptions,
		"DESCRIBE": SessionStateDescribe,
		"SETUP":    SessionStateSetup,
		"PLAY":     SessionStatePlay,
		"TEARDOWN": SessionStateTeardown,
		"PAUSE":    SessionStatePause,
	}
}

type session struct {
	conn net.Conn

	sink_       *sink
	sessionId   string
	writeBuffer *bytes.Buffer //响应体缓冲区
	state       SessionState
}

func (s *session) response(response *http.Response, body []byte) error {
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

func (s *session) close() {
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}

	if s.sink_ != nil {
		s.sink_.Close()
		s.sink_ = nil
	}
}

// 解析rtsp消息
func parseMessage(data []byte) (string, *url.URL, textproto.MIMEHeader, error) {
	reader := bufio.NewReader(bytes.NewReader(data))
	tp := textproto.NewReader(reader)
	line, err := tp.ReadLine()
	split := strings.Split(line, " ")
	if len(split) < 3 {
		panic(fmt.Errorf("wrong request line %s", line))
	}

	method := strings.ToUpper(split[0])
	//version
	_ = split[2]

	url_, err := url.Parse(split[1])
	if err != nil {
		return "", nil, nil, err
	}

	path := strings.TrimSpace(url_.Path)
	if strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	if len(strings.TrimSpace(path)) == 0 {
		return "", nil, nil, fmt.Errorf("the request source cannot be empty")
	}

	header, err := tp.ReadMIMEHeader()
	if err != nil {
		return "", nil, nil, err
	}

	return method, url_, header, nil
}

func NewSession(conn net.Conn) *session {
	milli := int(time.Now().UnixMilli() & 0xFFFFFFFF)
	return &session{
		conn:        conn,
		sessionId:   strconv.Itoa(milli),
		writeBuffer: bytes.NewBuffer(make([]byte, 0, 1024*10)),
		state:       SessionStateOptions,
	}
}

func NewResponse(code int, cseq string) *http.Response {
	rep := http.Response{
		Proto:      Version,
		StatusCode: code,
		Status:     http.StatusText(code),
		Header:     make(http.Header),
	}

	if cseq == "" {
		cseq = "1"
	}
	rep.Header.Set("Cseq", cseq)
	return &rep
}

func NewOKResponse(cseq string) *http.Response {
	return NewResponse(http.StatusOK, cseq)
}
