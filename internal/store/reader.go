package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Reader abre la DB de engram en modo read-only.
type Reader struct {
	db *sql.DB
}

// Open abre la DB en modo read-only con WAL y timeout de 5s.
func Open(dbPath string) (*Reader, error) {
	if strings.HasPrefix(dbPath, "~/") {
		home, _ := os.UserHomeDir()
		dbPath = filepath.Join(home, dbPath[2:])
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=query_only(1)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	return &Reader{db: db}, nil
}

// Close cierra la conexión.
func (r *Reader) Close() error {
	return r.db.Close()
}

// Export lee todas las observations y sessions de la DB.
// Replica exactamente la query de engram store.Export().
func (r *Reader) Export() (*ExportData, error) {
	sessions, err := r.querySessions()
	if err != nil {
		return nil, fmt.Errorf("export sessions: %w", err)
	}
	observations, err := r.queryObservations()
	if err != nil {
		return nil, fmt.Errorf("export observations: %w", err)
	}
	return &ExportData{
		Version:      "1",
		ExportedAt:   time.Now().UTC().Format(time.RFC3339),
		Sessions:     sessions,
		Observations: observations,
	}, nil
}

func (r *Reader) querySessions() ([]Session, error) {
	rows, err := r.db.Query(`
		SELECT id, project, directory, started_at, ended_at, summary
		FROM sessions
		ORDER BY started_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Project, &s.Directory, &s.StartedAt, &s.EndedAt, &s.Summary); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Reader) queryObservations() ([]Observation, error) {
	rows, err := r.db.Query(`
		SELECT id, ifnull(sync_id,'') as sync_id, session_id, type, title,
		       content, tool_name, project, scope, topic_key,
		       revision_count, duplicate_count, last_seen_at,
		       created_at, updated_at, deleted_at
		FROM observations
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Observation
	for rows.Next() {
		var o Observation
		if err := rows.Scan(
			&o.ID, &o.SyncID, &o.SessionID, &o.Type, &o.Title,
			&o.Content, &o.ToolName, &o.Project, &o.Scope, &o.TopicKey,
			&o.RevisionCount, &o.DuplicateCount, &o.LastSeenAt,
			&o.CreatedAt, &o.UpdatedAt, &o.DeletedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
