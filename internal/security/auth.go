package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Role string

const (
	RoleViewer     Role = "viewer"
	RoleOperator   Role = "operator"
	RoleSupervisor Role = "supervisor"
	RoleAdmin      Role = "admin"
)

type Principal struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     Role   `json:"role"`
	Bootstrap bool  `json:"bootstrap,omitempty"`
}

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name,omitempty"`
	Role         Role      `json:"role"`
	Enabled      bool      `json:"enabled"`
	PasswordHash string    `json:"password_hash,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastLoginAt  time.Time `json:"last_login_at,omitempty"`
}

type UserPublic struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name,omitempty"`
	Role        Role      `json:"role"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastLoginAt time.Time `json:"last_login_at,omitempty"`
}

func (u User) Public() UserPublic {
	return UserPublic{ID:u.ID,Username:u.Username,DisplayName:u.DisplayName,Role:u.Role,Enabled:u.Enabled,CreatedAt:u.CreatedAt,UpdatedAt:u.UpdatedAt,LastLoginAt:u.LastLoginAt}
}

type session struct {
	Principal Principal
	ExpiresAt time.Time
}

type AuthManager struct {
	mu sync.RWMutex
	path string
	bootstrapToken string
	users map[string]User
	byUsername map[string]string
	sessions map[string]session
	sessionTTL time.Duration
}

type diskUsers struct {
	Version int `json:"version"`
	Users []User `json:"users"`
}

func OpenAuthManager(path, bootstrapToken string) (*AuthManager,error) {
	m:=&AuthManager{
		path:path,bootstrapToken:bootstrapToken,
		users:make(map[string]User),byUsername:make(map[string]string),
		sessions:make(map[string]session),sessionTTL:12*time.Hour,
	}
	if err:=m.load(); err!=nil { return nil,err }
	return m,nil
}

func (m *AuthManager) AuthenticateBearer(token string) (Principal,bool) {
	token=strings.TrimSpace(token)
	if token=="" { return Principal{},false }
	if subtle.ConstantTimeCompare([]byte(token),[]byte(m.bootstrapToken))==1 {
		return Principal{UserID:"bootstrap",Username:"bootstrap-admin",Role:RoleAdmin,Bootstrap:true},true
	}
	sum:=sha256.Sum256([]byte(token))
	key:=hex.EncodeToString(sum[:])
	now:=time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	s,ok:=m.sessions[key]
	if !ok { return Principal{},false }
	if now.After(s.ExpiresAt) {
		delete(m.sessions,key)
		return Principal{},false
	}
	u,ok:=m.users[s.Principal.UserID]
	if !ok || !u.Enabled || u.Role!=s.Principal.Role {
		delete(m.sessions,key)
		return Principal{},false
	}
	return s.Principal,true
}

func (m *AuthManager) Login(username,password string) (string,Principal,time.Time,error) {
	username=normalizeUsername(username)
	m.mu.Lock()
	defer m.mu.Unlock()
	id,ok:=m.byUsername[username]
	if !ok { return "",Principal{},time.Time{},errors.New("invalid username or password") }
	u:=m.users[id]
	if !u.Enabled { return "",Principal{},time.Time{},errors.New("user is disabled") }
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash),[]byte(password))!=nil {
		return "",Principal{},time.Time{},errors.New("invalid username or password")
	}
	raw:=make([]byte,32)
	if _,err:=rand.Read(raw); err!=nil { return "",Principal{},time.Time{},err }
	token:=base64.RawURLEncoding.EncodeToString(raw)
	sum:=sha256.Sum256([]byte(token))
	expires:=time.Now().Add(m.sessionTTL).UTC()
	p:=Principal{UserID:u.ID,Username:u.Username,Role:u.Role}
	m.sessions[hex.EncodeToString(sum[:])]=session{Principal:p,ExpiresAt:expires}
	u.LastLoginAt=time.Now().UTC()
	u.UpdatedAt=u.LastLoginAt
	m.users[id]=u
	if err:=m.persistLocked(); err!=nil { return "",Principal{},time.Time{},err }
	return token,p,expires,nil
}

func (m *AuthManager) Logout(token string) {
	sum:=sha256.Sum256([]byte(strings.TrimSpace(token)))
	m.mu.Lock()
	delete(m.sessions,hex.EncodeToString(sum[:]))
	m.mu.Unlock()
}

func (m *AuthManager) ListUsers() []UserPublic {
	m.mu.RLock(); defer m.mu.RUnlock()
	out:=make([]UserPublic,0,len(m.users))
	for _,u:=range m.users { out=append(out,u.Public()) }
	sort.Slice(out,func(i,j int) bool { return out[i].Username<out[j].Username })
	return out
}

func (m *AuthManager) CreateUser(username,displayName,password string,role Role) (UserPublic,error) {
	username=normalizeUsername(username)
	if err:=validateUser(username,password,role); err!=nil { return UserPublic{},err }
	hash,err:=bcrypt.GenerateFromPassword([]byte(password),bcrypt.DefaultCost)
	if err!=nil { return UserPublic{},err }
	now:=time.Now().UTC()
	u:=User{ID:randomAuthID(),Username:username,DisplayName:strings.TrimSpace(displayName),Role:role,Enabled:true,PasswordHash:string(hash),CreatedAt:now,UpdatedAt:now}
	m.mu.Lock(); defer m.mu.Unlock()
	if _,exists:=m.byUsername[username]; exists { return UserPublic{},errors.New("username already exists") }
	m.users[u.ID]=u
	m.byUsername[username]=u.ID
	if err:=m.persistLocked(); err!=nil { delete(m.users,u.ID); delete(m.byUsername,username); return UserPublic{},err }
	return u.Public(),nil
}

func (m *AuthManager) UpdateUser(id,displayName string,role Role,enabled *bool) (UserPublic,error) {
	if !ValidRole(role) { return UserPublic{},errors.New("invalid role") }
	m.mu.Lock(); defer m.mu.Unlock()
	u,ok:=m.users[id]
	if !ok { return UserPublic{},os.ErrNotExist }
	u.DisplayName=strings.TrimSpace(displayName)
	u.Role=role
	if enabled!=nil { u.Enabled=*enabled }
	u.UpdatedAt=time.Now().UTC()
	m.users[id]=u
	if err:=m.persistLocked(); err!=nil { return UserPublic{},err }
	return u.Public(),nil
}

func (m *AuthManager) SetPassword(id,password string) error {
	if len(password)<10 { return errors.New("password must have at least 10 characters") }
	hash,err:=bcrypt.GenerateFromPassword([]byte(password),bcrypt.DefaultCost)
	if err!=nil { return err }
	m.mu.Lock(); defer m.mu.Unlock()
	u,ok:=m.users[id]
	if !ok { return os.ErrNotExist }
	u.PasswordHash=string(hash)
	u.UpdatedAt=time.Now().UTC()
	m.users[id]=u
	for key,s:=range m.sessions { if s.Principal.UserID==id { delete(m.sessions,key) } }
	return m.persistLocked()
}

func (m *AuthManager) DeleteUser(id string) error {
	m.mu.Lock(); defer m.mu.Unlock()
	u,ok:=m.users[id]
	if !ok { return os.ErrNotExist }
	delete(m.users,id); delete(m.byUsername,u.Username)
	for key,s:=range m.sessions { if s.Principal.UserID==id { delete(m.sessions,key) } }
	return m.persistLocked()
}

func Authorize(role Role, permission string) bool {
	if role==RoleAdmin { return true }
	switch permission {
	case "view":
		return role==RoleViewer || role==RoleOperator || role==RoleSupervisor
	case "operate":
		return role==RoleOperator || role==RoleSupervisor
	case "evidence":
		return role==RoleSupervisor
	case "admin":
		return false
	default:
		return false
	}
}

func ValidRole(role Role) bool {
	return role==RoleViewer || role==RoleOperator || role==RoleSupervisor || role==RoleAdmin
}

func validateUser(username,password string,role Role) error {
	if len(username)<3 || len(username)>64 { return errors.New("username must have 3 to 64 characters") }
	if len(password)<10 { return errors.New("password must have at least 10 characters") }
	if !ValidRole(role) { return errors.New("invalid role") }
	return nil
}

func normalizeUsername(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func (m *AuthManager) load() error {
	content,err:=os.ReadFile(m.path)
	if errors.Is(err,os.ErrNotExist) { return nil }
	if err!=nil { return err }
	var disk diskUsers
	if err:=json.Unmarshal(content,&disk); err!=nil { return fmt.Errorf("decode users: %w",err) }
	for _,u:=range disk.Users {
		if u.ID=="" || u.Username=="" || !ValidRole(u.Role) { continue }
		u.Username=normalizeUsername(u.Username)
		m.users[u.ID]=u
		m.byUsername[u.Username]=u.ID
	}
	return nil
}

func (m *AuthManager) persistLocked() error {
	if err:=os.MkdirAll(filepath.Dir(m.path),0o750); err!=nil { return err }
	users:=make([]User,0,len(m.users))
	for _,u:=range m.users { users=append(users,u) }
	sort.Slice(users,func(i,j int) bool { return users[i].ID<users[j].ID })
	payload,err:=json.MarshalIndent(diskUsers{Version:1,Users:users},"","  ")
	if err!=nil { return err }
	tmp:=m.path+".tmp"
	if err:=os.WriteFile(tmp,append(payload,'
'),0o600); err!=nil { return err }
	f,err:=os.OpenFile(tmp,os.O_WRONLY,0)
	if err==nil { _=f.Sync(); _=f.Close() }
	if err:=os.Rename(tmp,m.path); err!=nil { return err }
	return os.Chmod(m.path,0o600)
}

func randomAuthID() string {
	var b [16]byte
	if _,err:=rand.Read(b[:]); err!=nil { return fmt.Sprintf("%d",time.Now().UnixNano()) }
	return hex.EncodeToString(b[:])
}
