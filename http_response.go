package main

import (
	"encoding/json"
	"net/http"
)

func httpResponse(w http.ResponseWriter, code int, msg string) {
	httpResponse2(w, MalformedRequest{
		Code: code,
		Msg:  msg,
	})
}

func httpResponseOk(w http.ResponseWriter, data interface{}) {
	httpResponse2(w, MalformedRequest{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: data,
	})
}

func httpResponse2(w http.ResponseWriter, payload interface{}) {
	body, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT")
	w.Write(body)
}
