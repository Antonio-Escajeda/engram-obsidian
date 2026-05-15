package obsidian

import (
	"encoding/json"
	"os"
	"time"
)

// SyncState persiste el estado del último export incremental.
type SyncState struct {
	LastExportAt string           `json:"last_export_at"`
	Files        map[int64]string `json:"files"`   // obs ID → relative path
	Version      int              `json:"version"`
}

// ExportResult resume el resultado de un ciclo de export.
type ExportResult struct {
	Created int
	Updated int
	Deleted int
	Skipped int
	Errors  []error
}

// ReadState carga el state desde disco. Devuelve un SyncState vacío si no existe.
func ReadState(path string) (SyncState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return SyncState{
			Files:   make(map[int64]string),
			Version: 1,
		}, nil
	}
	if err != nil {
		return SyncState{}, err
	}
	var s SyncState
	if err := json.Unmarshal(data, &s); err != nil {
		return SyncState{}, err
	}
	if s.Files == nil {
		s.Files = make(map[int64]string)
	}
	return s, nil
}

// WriteState persiste el state a disco.
func WriteState(path string, s SyncState) error {
	s.LastExportAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
