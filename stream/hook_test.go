package stream

import (
	"net/http"
	"testing"
)

func TestHookServer(t *testing.T) {

	http.HandleFunc("/api/v1/live/publish/auth", func(writer http.ResponseWriter, request *http.Request) {
		if true {
			writer.WriteHeader(http.StatusOK)
		} else {
			writer.WriteHeader(http.StatusNonAuthoritativeInfo)
		}
	})

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		panic(err)
	}
}
