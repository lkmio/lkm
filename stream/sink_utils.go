package stream

import (
	"encoding/binary"
	"net"
	"strconv"
)

// SinkID IPV4使用uint64、IPV6使用string作为ID类型
type SinkID interface{}

type IPV4SinkID uint64

type IPV6SinkID string

func ipv4Addr2UInt64(ip uint32, port int) uint64 {
	return (uint64(ip) << 32) | uint64(port)
}

// NetAddr2SinkId 根据网络地址生成SinkId IPV4使用一个uint64, IPV6使用String
func NetAddr2SinkId(addr net.Addr) SinkID {
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

func SinkId2String(id SinkID) string {
	if i, ok := id.(uint64); ok {
		return strconv.FormatUint(i, 10)
	}

	return id.(string)
}
