package media

import (
	"strings"
	"testing"
)

func TestHTTPDigestAuthorization(t *testing.T) {
	challenge := `Digest realm="cam", nonce="abcdef", qop="auth", algorithm=MD5, opaque="xyz"`
	auth, err := buildHTTPDigestAuthorization(challenge, "admin", "secret", "GET", "/ISAPI/Streaming/channels/101/picture")
	if err != nil { t.Fatal(err) }
	for _, part := range []string{"Digest ", `username="admin"`, `realm="cam"`, "response=", "qop=auth", "nc=00000001"} {
		if !strings.Contains(auth, part) { t.Fatalf("missing %q in %q", part, auth) }
	}
}

func TestParseHTTPDigestParamsQuotedComma(t *testing.T) {
	params := parseHTTPDigestParams(`realm="camera,01", nonce="abc", qop="auth,auth-int"`)
	if params["realm"] != "camera,01" { t.Fatalf("realm=%q", params["realm"]) }
	if params["nonce"] != "abc" { t.Fatalf("nonce=%q", params["nonce"]) }
	if chooseHTTPQOP(params["qop"]) != "auth" { t.Fatalf("qop=%q", params["qop"]) }
}
