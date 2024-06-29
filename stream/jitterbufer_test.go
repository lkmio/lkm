package stream

import (
	"math/rand"
	"strconv"
	"testing"
	"time"
)

func TestName(t *testing.T) {
	// 设置随机数种子，确保每次运行程序时都能得到不同的随机序列
	rand.Seed(time.Now().UnixNano())
	buffer := NewJitterBuffer(func(packet interface{}) {
		println(packet.(string))
	})

	for i := 0; i < 65535; i++ {
		// 生成1到65535之间的随机数
		randomNumber := rand.Intn(65535) + 1
		buffer.Push(uint16(randomNumber), strconv.Itoa(randomNumber))
	}

	buffer.Flush()
	buffer.Close()
}
