package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

func (s *Server) handlePluginStatus(w http.ResponseWriter,r *http.Request){
	ctx,cancel:=context.WithTimeout(r.Context(),2*time.Second)
	defer cancel()
	req,err:=http.NewRequestWithContext(ctx,http.MethodGet,"http://127.0.0.1:8091/healthz",nil)
	if err!=nil{writeError(w,http.StatusInternalServerError,err.Error());return}
	client:=&http.Client{Timeout:2*time.Second}
	resp,err:=client.Do(req)
	if err!=nil{
		writeJSON(w,http.StatusOK,map[string]any{"id":"plate-ocr","state":"offline","error":err.Error()})
		return
	}
	defer resp.Body.Close()
	var status map[string]any
	if err:=json.NewDecoder(io.LimitReader(resp.Body,1<<20)).Decode(&status);err!=nil{
		writeJSON(w,http.StatusOK,map[string]any{"id":"plate-ocr","state":"invalid","error":err.Error()})
		return
	}
	status["id"]="plate-ocr"
	status["http_status"]=resp.StatusCode
	writeJSON(w,http.StatusOK,status)
}
