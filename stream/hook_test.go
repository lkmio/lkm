package stream

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestHookServer(t *testing.T) {
	//模拟各种多个情况对推拉流的影响
	random := false
	i := 1
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		println(fmt.Sprintf("接收到请求 path:%s", request.URL.Path))
		if !random {
			writer.WriteHeader(http.StatusOK)
			return
		}

		switch i {
		case 1:
			writer.WriteHeader(http.StatusOK)
			break
		case 2:
			writer.WriteHeader(http.StatusNonAuthoritativeInfo)
			break
		case 3:
			time.Sleep(5 * time.Second)
			break
		case 4:
			time.Sleep(20 * time.Second)
			break
		}

		i = i%5 + 1
	})

	err := http.ListenAndServe(":8082", nil)
	if err != nil {
		panic(err)
	}
}
