package httpapi

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/wfuzatto/Nvr/internal/security"
)

type loginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter,r *http.Request) {
	if s.deps.Auth==nil { writeError(w,http.StatusServiceUnavailable,"authentication unavailable"); return }
	var in loginInput
	if err:=decodeJSON(r,&in); err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	token,p,expires,err:=s.deps.Auth.Login(in.Username,in.Password)
	if err!=nil {
		s.auditEvent(r,security.Principal{Username:strings.ToLower(strings.TrimSpace(in.Username))},"auth.login","session","",false,nil)
		writeError(w,http.StatusUnauthorized,"invalid username or password")
		return
	}
	s.auditEvent(r,p,"auth.login","session","",true,nil)
	writeJSON(w,http.StatusOK,map[string]any{
		"token":token,
		"expires_at":expires,
		"user":p,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter,r *http.Request) {
	if s.deps.Auth!=nil {
		token:=bearerToken(r)
		if p,ok:=principalFromRequest(r); ok {
			s.auditEvent(r,p,"auth.logout","session","",true,nil)
		}
		s.deps.Auth.Logout(token)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter,r *http.Request) {
	p,ok:=principalFromRequest(r)
	if !ok { writeError(w,http.StatusUnauthorized,"not authenticated"); return }
	writeJSON(w,http.StatusOK,p)
}

func (s *Server) handleListUsers(w http.ResponseWriter,_ *http.Request) {
	writeJSON(w,http.StatusOK,map[string]any{"items":s.deps.Auth.ListUsers(),"count":len(s.deps.Auth.ListUsers())})
}

func (s *Server) handleCreateUser(w http.ResponseWriter,r *http.Request) {
	var in struct {
		Username string `json:"username"`
		DisplayName string `json:"display_name"`
		Password string `json:"password"`
		Role security.Role `json:"role"`
	}
	if err:=decodeJSON(r,&in); err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	user,err:=s.deps.Auth.CreateUser(in.Username,in.DisplayName,in.Password,in.Role)
	if err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	writeJSON(w,http.StatusCreated,user)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter,r *http.Request) {
	var in struct {
		DisplayName string `json:"display_name"`
		Role security.Role `json:"role"`
		Enabled *bool `json:"enabled"`
	}
	if err:=decodeJSON(r,&in); err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	user,err:=s.deps.Auth.UpdateUser(r.PathValue("id"),in.DisplayName,in.Role,in.Enabled)
	if errors.Is(err,os.ErrNotExist) { writeError(w,http.StatusNotFound,"user not found"); return }
	if err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	writeJSON(w,http.StatusOK,user)
}

func (s *Server) handleSetUserPassword(w http.ResponseWriter,r *http.Request) {
	var in struct{ Password string `json:"password"` }
	if err:=decodeJSON(r,&in); err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	err:=s.deps.Auth.SetPassword(r.PathValue("id"),in.Password)
	if errors.Is(err,os.ErrNotExist) { writeError(w,http.StatusNotFound,"user not found"); return }
	if err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter,r *http.Request) {
	p,_:=principalFromRequest(r)
	if p.UserID==r.PathValue("id") { writeError(w,http.StatusConflict,"cannot delete the currently authenticated user"); return }
	err:=s.deps.Auth.DeleteUser(r.PathValue("id"))
	if errors.Is(err,os.ErrNotExist) { writeError(w,http.StatusNotFound,"user not found"); return }
	if err!=nil { writeError(w,http.StatusInternalServerError,err.Error()); return }
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAuditList(w http.ResponseWriter,r *http.Request) {
	limit:=200
	if raw:=r.URL.Query().Get("limit"); raw!="" {
		value,err:=strconv.Atoi(raw)
		if err!=nil || value<1 || value>5000 { writeError(w,http.StatusBadRequest,"limit must be between 1 and 5000"); return }
		limit=value
	}
	items,err:=s.deps.Audit.List(limit)
	if err!=nil { writeError(w,http.StatusInternalServerError,err.Error()); return }
	writeJSON(w,http.StatusOK,map[string]any{"items":items,"count":len(items)})
}

func (s *Server) handleAuditVerify(w http.ResponseWriter,_ *http.Request) {
	if err:=s.deps.Audit.Verify(); err!=nil {
		writeJSON(w,http.StatusConflict,map[string]any{"valid":false,"error":err.Error()})
		return
	}
	writeJSON(w,http.StatusOK,map[string]any{"valid":true})
}
