package playback

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

func SignToken(secret, cameraID string, ttl time.Duration) string {
	if ttl <= 0 { ttl = 10*time.Minute }
	exp := time.Now().Add(ttl).Unix()
	message := cameraID+"|"+strconv.FormatInt(exp,10)
	mac := hmac.New(sha256.New,[]byte(secret))
	_,_ = mac.Write([]byte(message))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return strconv.FormatInt(exp,10)+"."+sig
}

func ValidateToken(secret, cameraID, token string) error {
	parts:=strings.Split(token,".")
	if len(parts)!=2 { return errors.New("invalid playback token") }
	exp,err:=strconv.ParseInt(parts[0],10,64)
	if err!=nil { return errors.New("invalid playback token") }
	if time.Now().Unix()>exp { return errors.New("playback token expired") }
	message:=cameraID+"|"+parts[0]
	mac:=hmac.New(sha256.New,[]byte(secret))
	_,_=mac.Write([]byte(message))
	expected:=mac.Sum(nil)
	got,err:=base64.RawURLEncoding.DecodeString(parts[1])
	if err!=nil || !hmac.Equal(got,expected) { return errors.New("invalid playback token") }
	return nil
}
