package stream

import (
	"encoding/binary"
	"fmt"
	"github.com/lkmio/avformat/utils"
	"github.com/lkmio/transport"
	"net"
	"net/url"
	"strconv"
	"time"
)

// SinkID IPV4使用uint64、IPV6使用string作为ID类型
type SinkID interface{}

type IPV4SinkID uint64

type IPV6SinkID string

func ipv4Addr2UInt64(ip uint32, port int) uint64 {
	return (uint64(ip) << 32) | uint64(port)
}

// NetAddr2SinkID 根据网络地址生成SinkId IPV4使用一个uint64, IPV6使用String
func NetAddr2SinkID(addr net.Addr) SinkID {
	network := addr.Network()
	if "tcp" == network {
		to4 := addr.(*net.TCPAddr).IP.To4()
		var intIP uint32
		if to4 != nil {
			intIP = binary.BigEndian.Uint32(to4)
		}

		return ipv4Addr2UInt64(intIP, addr.(*net.TCPAddr).Port)
	} else if "udp" == network {
		to4 := addr.(*net.UDPAddr).IP.To4()
		var intIP uint32
		if to4 != nil {
			intIP = binary.BigEndian.Uint32(to4)
		}

		return ipv4Addr2UInt64(intIP, addr.(*net.UDPAddr).Port)
	}

	return addr.String()
}

func SinkID2String(id SinkID) string {
	if i, ok := id.(uint64); ok {
		return strconv.FormatUint(i, 10)
	}

	return id.(string)
}

func GenerateUint64SinkID() SinkID {
	return uint64(time.Now().UnixNano()&0xFFFFFFFF)<<32 | uint64(utils.RandomIntInRange(0, 0xFFFFFFFF))
}

func CreateSinkDisconnectionMessage(sink Sink) string {
	return fmt.Sprintf("%s sink断开连接. id: %s", sink.GetProtocol(), sink.GetID())
}

func ExecuteSyncEventOnTransStreamPublisher(sourceId string, event func()) bool {
	source := SourceManager.Find(sourceId)
	if source != nil {
		source.GetTransStreamPublisher().ExecuteSyncEvent(event)
		return true
	}

	return false
}

func SubscribeStream(sink Sink, values url.Values) utils.HookState {
	return SubscribeStreamWithOptions(sink, values, true, false)
}

func SubscribeStreamWithOptions(sink Sink, values url.Values, ready bool, timeout bool) utils.HookState {
	sink.SetReady(ready)
	sink.SetUrlValues(values)
	_, state := PreparePlaySink(sink, timeout)
	return state
}

func ForwardStream(protocol TransStreamProtocol, transport TransportType, sourceId string, values url.Values, remoteAddr string, manager transport.Manager, ssrc uint32) (Sink, int, error) {
	//source := SourceManager.Find(sourceId)
	//if source == nil {
	//	return nil, 0, fmt.Errorf("source %s 不存在", sourceId)
	//}

	sinkId := GenerateUint64SinkID()
	var port int
	sink, port, err := NewForwardSink(transport, protocol, sinkId, sourceId, remoteAddr, manager, ssrc)
	if err != nil {
		return nil, 0, err
	}

	state := SubscribeStreamWithOptions(sink, values, true, true)
	if utils.HookStateOK != state {
		sink.Close()
		return nil, 0, fmt.Errorf("failed to prepare play sink")
	}

	return sink, port, nil
}
