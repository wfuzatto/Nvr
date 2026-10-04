package model

import "time"

type HotlistEntry struct {
	ID        string    `json:"id"`
	Plate     string    `json:"plate"`
	Label     string    `json:"label"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
