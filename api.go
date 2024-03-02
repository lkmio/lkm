package main

import (
	"github.com/gorilla/mux"
	"github.com/yangjiechina/avformat/utils"
	"github.com/yangjiechina/live-server/hls"
	"github.com/yangjiechina/live-server/stream"
	"net/http"
	"time"
)

func startApiServer(addr string) {
	r := mux.NewRouter()
	r.HandleFunc("/live/hls/{id}", onHLS)
	http.Handle("/", r)

	srv := &http.Server{
		Handler: r,
		Addr:    addr,
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	err := srv.ListenAndServe()

	if err != nil {
		panic(err)
	}
}

func onHLS(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sourceId := vars["id"]

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
		return
	}

	conn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	sinkId := stream.GenerateSinkId(conn)

	/*	requestTS := strings.HasSuffix(r.URL.Path, ".ts")
		if requestTS {
			stream.sink
		}*/

	sink := hls.NewSink(sinkId, sourceId, w)
	sink.(*stream.SinkImpl).Play(sink, func() {

	}, func(state utils.HookState) {
		w.WriteHeader(http.StatusForbidden)
	})

}
