package security

import "testing"

func TestAuthUsersAndRoles(t *testing.T) {
	m,err:=OpenAuthManager(t.TempDir()+"/users.json","bootstrap-secret")
	if err!=nil { t.Fatal(err) }
	if p,ok:=m.AuthenticateBearer("bootstrap-secret"); !ok || p.Role!=RoleAdmin || !p.Bootstrap { t.Fatalf("bootstrap=%+v ok=%v",p,ok) }

	u,err:=m.CreateUser("Operator","Camera Operator","long-password-123",RoleOperator)
	if err!=nil { t.Fatal(err) }
	if u.Username!="operator" { t.Fatalf("username=%q",u.Username) }

	token,p,_,err:=m.Login("operator","long-password-123")
	if err!=nil { t.Fatal(err) }
	if p.Role!=RoleOperator { t.Fatalf("role=%s",p.Role) }
	if got,ok:=m.AuthenticateBearer(token); !ok || got.UserID!=u.ID { t.Fatalf("auth=%+v ok=%v",got,ok) }
	if !Authorize(RoleOperator,"view") || !Authorize(RoleOperator,"operate") || Authorize(RoleOperator,"admin") { t.Fatal("role matrix invalid") }

	m.Logout(token)
	if _,ok:=m.AuthenticateBearer(token); ok { t.Fatal("logout token still valid") }
}
