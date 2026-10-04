package security

import (
	"path/filepath"
	"testing"
)

func TestSecretBoxRoundTrip(t *testing.T) {
	box, err := LoadOrCreateSecretBox(filepath.Join(t.TempDir(), "master.key"))
	if err != nil { t.Fatal(err) }

	want := "rtsp://user:password@10.0.0.1/stream"
	enc, err := box.Encrypt(want)
	if err != nil { t.Fatal(err) }
	if enc == want { t.Fatal("ciphertext equals cleartext") }

	got, err := box.Decrypt(enc)
	if err != nil { t.Fatal(err) }
	if got != want { t.Fatalf("got %q want %q", got, want) }
}
