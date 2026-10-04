package model

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

type Camera struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Description         string    `json:"description,omitempty"`
	City                string    `json:"city,omitempty"`
	Site                string    `json:"site,omitempty"`
	Latitude            *float64  `json:"latitude,omitempty"`
	Longitude           *float64  `json:"longitude,omitempty"`
	Enabled             bool      `json:"enabled"`
	RTSPURLCipher       string    `json:"rtsp_url_cipher"`
	RTSPURLRedacted     string    `json:"rtsp_url_redacted"`
	SnapshotURLCipher   string    `json:"snapshot_url_cipher,omitempty"`
	SnapshotURLRedacted string    `json:"snapshot_url_redacted,omitempty"`
	ONVIFURLCipher      string    `json:"onvif_url_cipher,omitempty"`
	ONVIFURLRedacted    string    `json:"onvif_url_redacted,omitempty"`
	ONVIFProfileToken   string    `json:"onvif_profile_token,omitempty"`
	ONVIFMediaVersion   int       `json:"onvif_media_version,omitempty"`
	ONVIFPTZ            bool      `json:"onvif_ptz,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type CameraPublic struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Description         string    `json:"description,omitempty"`
	City                string    `json:"city,omitempty"`
	Site                string    `json:"site,omitempty"`
	Latitude            *float64  `json:"latitude,omitempty"`
	Longitude           *float64  `json:"longitude,omitempty"`
	Enabled             bool      `json:"enabled"`
	RTSPURLRedacted     string    `json:"rtsp_url"`
	SnapshotURLRedacted string    `json:"snapshot_url,omitempty"`
	ONVIFURLRedacted    string    `json:"onvif_url,omitempty"`
	ONVIFProfileToken   string    `json:"onvif_profile_token,omitempty"`
	ONVIFMediaVersion   int       `json:"onvif_media_version,omitempty"`
	ONVIFPTZ            bool      `json:"onvif_ptz,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (c Camera) Public() CameraPublic {
	return CameraPublic{
		ID: c.ID, Name: c.Name, Description: c.Description,
		City: c.City, Site: c.Site, Latitude: c.Latitude, Longitude: c.Longitude,
		Enabled: c.Enabled, RTSPURLRedacted: c.RTSPURLRedacted,
		SnapshotURLRedacted: c.SnapshotURLRedacted,
		ONVIFURLRedacted: c.ONVIFURLRedacted,
		ONVIFProfileToken: c.ONVIFProfileToken,
		ONVIFMediaVersion: c.ONVIFMediaVersion,
		ONVIFPTZ: c.ONVIFPTZ,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil { panic(err) }
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	raw := hex.EncodeToString(b[:])
	return strings.Join([]string{raw[0:8], raw[8:12], raw[12:16], raw[16:20], raw[20:32]}, "-")
}
