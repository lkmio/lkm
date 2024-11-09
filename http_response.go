package main

import (
	"encoding/json"
	"net/http"
)

func httpResponse(w http.ResponseWriter, code int, msg string) {
	httpResponseJson(w, MalformedRequest{
		Code: code,
		Msg:  msg,
	})
}

func httpResponseJson(w http.ResponseWriter, payload interface{}) {
	body, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT")
	w.Write(body)
}

func httpResponseOK(w http.ResponseWriter, data interface{}) {
	httpResponseJson(w, MalformedRequest{
		Code: http.StatusOK,
		Msg:  "ok",
		Data: data,
	})
}

func httpResponseError(w http.ResponseWriter, msg string) {
	httpResponseJson(w, MalformedRequest{
		Code: -1,
		Msg:  msg,
		Data: nil,
	})
}
