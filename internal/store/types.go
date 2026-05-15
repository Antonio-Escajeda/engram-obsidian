package store

import "time"

type Session struct {
	ID        string  `json:"id"`
	Project   string  `json:"project"`
	Directory string  `json:"directory"`
	StartedAt string  `json:"started_at"`
	EndedAt   *string `json:"ended_at,omitempty"`
	Summary   *string `json:"summary,omitempty"`
}

type Observation struct {
	ID             int64   `json:"id"`
	SyncID         string  `json:"sync_id"`
	SessionID      string  `json:"session_id"`
	Type           string  `json:"type"`
	Title          string  `json:"title"`
	Content        string  `json:"content"`
	ToolName       *string `json:"tool_name,omitempty"`
	Project        *string `json:"project,omitempty"`
	Scope          string  `json:"scope"`
	TopicKey       *string `json:"topic_key,omitempty"`
	RevisionCount  int     `json:"revision_count"`
	DuplicateCount int     `json:"duplicate_count"`
	LastSeenAt     *string `json:"last_seen_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
}

type ExportData struct {
	Version      string        `json:"version"`
	ExportedAt   string        `json:"exported_at"`
	Sessions     []Session     `json:"sessions"`
	Observations []Observation `json:"observations"`
}

// ProjectName devuelve el project del observation, nunca nil.
func (o Observation) ProjectName() string {
	if o.Project != nil {
		return *o.Project
	}
	return "unknown"
}

// TopicKeyStr devuelve el topic_key del observation, nunca nil.
func (o Observation) TopicKeyStr() string {
	if o.TopicKey != nil {
		return *o.TopicKey
	}
	return ""
}

// CreatedMonth devuelve "YYYY-MM" del created_at, o "" si no tiene fecha.
func (o Observation) CreatedMonth() string {
	if len(o.CreatedAt) >= 7 {
		return o.CreatedAt[:7]
	}
	return ""
}

// CreatedYear devuelve "YYYY" del created_at, o "" si no tiene fecha.
func (o Observation) CreatedYear() string {
	if len(o.CreatedAt) >= 4 {
		return o.CreatedAt[:4]
	}
	return ""
}

// IsDeleted devuelve true si el observation tiene deleted_at.
func (o Observation) IsDeleted() bool {
	return o.DeletedAt != nil
}

// ExportedAtTime parsea ExportedAt como time.Time.
func (d ExportData) ExportedAtTime() (time.Time, error) {
	return time.Parse(time.RFC3339, d.ExportedAt)
}
