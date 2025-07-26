module github.com/lkmio/lkm

require (
	github.com/lkmio/audio-transcoder v0.2.1
	github.com/lkmio/flv v0.0.0
	github.com/lkmio/mpeg v0.0.0
	github.com/lkmio/rtmp v0.0.0
	github.com/lkmio/rtp v0.0.0
	github.com/lkmio/transport v0.0.0
)

require (
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.1
	github.com/lkmio/avformat v0.0.0
	github.com/lkmio/g726 v0.1.3
	github.com/natefinch/lumberjack v2.0.0+incompatible
	github.com/pion/interceptor v0.1.40
	github.com/pion/rtcp v1.2.15
	github.com/pion/rtp v1.8.21
	github.com/pion/sdp/v3 v3.0.14
	github.com/pion/webrtc/v4 v4.1.3
	github.com/sirupsen/logrus v1.9.3
	github.com/x-cray/logrus-prefixed-formatter v0.5.2
	go.uber.org/zap v1.27.0
)

require (
	github.com/BurntSushi/toml v1.3.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.16 // indirect
	github.com/mgutz/ansi v0.0.0-20200706080929-d51e80ef957d // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/pion/datachannel v1.5.10 // indirect
	github.com/pion/dtls/v3 v3.0.6 // indirect
	github.com/pion/ice/v4 v4.0.10 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.0.7 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/sctp v1.8.39 // indirect
	github.com/pion/srtp/v3 v3.0.6 // indirect
	github.com/pion/stun/v3 v3.0.0 // indirect
	github.com/pion/transport/v3 v3.0.7 // indirect
	github.com/pion/turn/v4 v4.0.0 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/crypto v0.33.0 // indirect
	golang.org/x/net v0.35.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/term v0.29.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

replace github.com/lkmio/avformat => ../avformat

replace github.com/lkmio/mpeg => ../mpeg

replace github.com/lkmio/flv => ../flv

replace github.com/lkmio/rtmp => ../rtmp

replace github.com/lkmio/transport => ../transport

replace github.com/lkmio/rtp => ../rtp

go 1.19
