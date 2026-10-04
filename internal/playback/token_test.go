package playback

import (
	"testing"
	"time"
)

func TestPlaybackToken(t *testing.T) {
	token:=SignToken("secret","cam-1",time.Minute)
	if err:=ValidateToken("secret","cam-1",token); err!=nil { t.Fatal(err) }
	if err:=ValidateToken("secret","cam-2",token); err==nil { t.Fatal("token should be camera-bound") }
}
