package model

import (
	"encoding/json"
	"time"
)

type EventEnvelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion string          `json:"schema_version"`
	TenantID      string          `json:"tenant_id,omitempty"`
	CityID        string          `json:"city_id,omitempty"`
	SiteID        string          `json:"site_id,omitempty"`
	NodeID        string          `json:"node_id,omitempty"`
	CameraID      string          `json:"camera_id"`
	ObservedAt    time.Time       `json:"observed_at"`
	ReceivedAt    time.Time       `json:"received_at"`
	PluginID      string          `json:"plugin_id"`
	PluginVersion string          `json:"plugin_version"`
	Confidence    float64         `json:"confidence"`
	SnapshotRef   string          `json:"snapshot_ref,omitempty"`
	ClipRef       string          `json:"clip_ref,omitempty"`
	Attributes    json.RawMessage `json:"attributes"`
	DedupeKey     string          `json:"dedupe_key,omitempty"`
}

type PlateAttributes struct {
	TrackID            string          `json:"track_id"`
	RawText            string          `json:"raw_text"`
	NormalizedText     string          `json:"normalized_text"`
	Format             string          `json:"format"`
	Candidates         json.RawMessage `json:"candidates,omitempty"`
	DetectorConfidence float64         `json:"detector_confidence"`
	BBox               json.RawMessage `json:"bbox,omitempty"`
	Lane               string          `json:"lane,omitempty"`
	Direction          string          `json:"direction,omitempty"`
}

func (e EventEnvelope) Plate() (PlateAttributes, error) {
	var p PlateAttributes
	err:=json.Unmarshal(e.Attributes,&p)
	return p,err
}
