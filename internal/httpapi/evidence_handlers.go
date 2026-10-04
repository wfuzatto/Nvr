package httpapi

import (
	"errors"
	"net/http"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/wfuzatto/Nvr/internal/store"
)

func (s *Server) handleCreateExport(w http.ResponseWriter,r *http.Request) {
	if s.deps.Evidence==nil { writeError(w,http.StatusServiceUnavailable,"evidence export unavailable"); return }
	var in struct {
		From string `json:"from"`
		To string `json:"to"`
	}
	if err:=decodeJSON(r,&in); err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	from,err:=time.Parse(time.RFC3339,in.From); if err!=nil { writeError(w,http.StatusBadRequest,"from must use RFC3339"); return }
	to,err:=time.Parse(time.RFC3339,in.To); if err!=nil { writeError(w,http.StatusBadRequest,"to must use RFC3339"); return }
	p,_:=principalFromRequest(r)
	job,err:=s.deps.Evidence.Create(r.PathValue("id"),from.UTC(),to.UTC(),p.Username)
	if errors.Is(err,store.ErrNotFound) { writeError(w,http.StatusNotFound,"camera not found"); return }
	if err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	writeJSON(w,http.StatusAccepted,job)
}

func (s *Server) handleListExports(w http.ResponseWriter,r *http.Request) {
	limit:=100
	if raw:=r.URL.Query().Get("limit"); raw!="" {
		value,err:=strconv.Atoi(raw)
		if err!=nil || value<1 || value>1000 { writeError(w,http.StatusBadRequest,"limit must be between 1 and 1000"); return }
		limit=value
	}
	items:=s.deps.Evidence.List(limit)
	writeJSON(w,http.StatusOK,map[string]any{"items":items,"count":len(items)})
}

func (s *Server) handleGetExport(w http.ResponseWriter,r *http.Request) {
	job,err:=s.deps.Evidence.Get(r.PathValue("export"))
	if errors.Is(err,store.ErrNotFound) { writeError(w,http.StatusNotFound,"export not found"); return }
	if err!=nil { writeError(w,http.StatusInternalServerError,err.Error()); return }
	writeJSON(w,http.StatusOK,job)
}

func (s *Server) handleDownloadExport(w http.ResponseWriter,r *http.Request) {
	path,job,err:=s.deps.Evidence.BundlePath(r.PathValue("export"))
	if errors.Is(err,store.ErrNotFound) { writeError(w,http.StatusNotFound,"export not found"); return }
	if err!=nil { writeError(w,http.StatusConflict,err.Error()); return }
	f,err:=os.Open(path)
	if err!=nil { writeError(w,http.StatusInternalServerError,err.Error()); return }
	defer f.Close()
	info,err:=f.Stat()
	if err!=nil { writeError(w,http.StatusInternalServerError,err.Error()); return }
	w.Header().Set("Content-Type","application/gzip")
	w.Header().Set("Content-Disposition",`attachment; filename="nvr-evidence-`+job.ID+`.tar.gz"`)
	w.Header().Set("Content-Length",strconv.FormatInt(info.Size(),10))
	w.Header().Set("X-Evidence-SHA256",job.BundleSHA256)
	w.Header().Set("Cache-Control","no-store")
	if p,ok:=principalFromRequest(r); ok {
		s.auditEvent(r,p,"evidence.download","export",job.ID,true,map[string]any{"sha256":job.BundleSHA256,"bytes":job.BundleBytes})
	}
	_,_=io.Copy(w,f)
}
