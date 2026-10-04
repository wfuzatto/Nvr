package httpapi

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/wfuzatto/Nvr/internal/audit"
	"github.com/wfuzatto/Nvr/internal/security"
)

type principalContextKey struct{}

func bearerToken(r *http.Request) string {
	auth:=strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth)<8 || !strings.EqualFold(auth[:7],"Bearer ") { return "" }
	return strings.TrimSpace(auth[7:])
}

func withPrincipal(r *http.Request,p security.Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(),principalContextKey{},p))
}

func principalFromRequest(r *http.Request) (security.Principal,bool) {
	p,ok:=r.Context().Value(principalContextKey{}).(security.Principal)
	return p,ok
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if s.deps.Auth==nil {
			writeError(w,http.StatusServiceUnavailable,"authentication unavailable")
			return
		}
		token:=bearerToken(r)
		p,ok:=s.deps.Auth.AuthenticateBearer(token)
		if !ok {
			w.Header().Set("WWW-Authenticate","Bearer")
			writeError(w,http.StatusUnauthorized,"invalid or missing bearer token")
			return
		}
		next.ServeHTTP(w,withPrincipal(r,p))
	})
}

func (s *Server) require(permission string,next http.Handler) http.Handler {
	return s.authenticate(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		p,_:=principalFromRequest(r)
		if !security.Authorize(p.Role,permission) {
			writeError(w,http.StatusForbidden,"permission denied")
			return
		}
		next.ServeHTTP(w,r)
	}))
}

// auth remains as the default read-only authenticated policy for existing routes.
func (s *Server) auth(next http.Handler) http.Handler { return s.require("view",next) }

type auditStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditStatusWriter) WriteHeader(status int) {
	w.status=status
	w.ResponseWriter.WriteHeader(status)
}
func (w *auditStatusWriter) Write(data []byte) (int,error) {
	if w.status==0 { w.status=http.StatusOK }
	return w.ResponseWriter.Write(data)
}

func (s *Server) audited(action,resource string,next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		aw:=&auditStatusWriter{ResponseWriter:w}
		next.ServeHTTP(aw,r)
		p,_:=principalFromRequest(r)
		id:=r.PathValue("id")
		if id=="" { id=r.PathValue("session") }
		s.auditEvent(r,p,action,resource,id,aw.status>0 && aw.status<400,map[string]any{"method":r.Method,"path":r.URL.Path,"status":aw.status})
	})
}

func (s *Server) auditEvent(r *http.Request,p security.Principal,action,resource,id string,success bool,details map[string]any) {
	if s.deps.Audit==nil { return }
	remote:=strings.TrimSpace(r.RemoteAddr)
	if host,_,err:=net.SplitHostPort(remote); err==nil { remote=host }
	_,_ = s.deps.Audit.Append(audit.Event{
		Time:time.Now().UTC(),ActorID:p.UserID,Actor:p.Username,Role:string(p.Role),
		Action:action,Resource:resource,ResourceID:id,RemoteAddr:remote,Success:success,Details:details,
	})
}
