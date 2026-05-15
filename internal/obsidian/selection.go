package obsidian

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

// SelectionMode indica si un nodo está seleccionado completo o parcialmente.
type SelectionMode string

const (
	SelectionFull    SelectionMode = "full"
	SelectionPartial SelectionMode = "partial"
)

// MonthSelection representa la selección dentro de un mes.
type MonthSelection struct {
	Mode    SelectionMode `json:"mode"`
	NoteIDs []int64       `json:"note_ids,omitempty"` // solo cuando mode == partial
}

// ProjectSelection representa la selección de un proyecto completo.
type ProjectSelection struct {
	Mode   SelectionMode             `json:"mode"`
	Months map[string]MonthSelection `json:"months,omitempty"` // clave: "YYYY-MM"
}

// Config almacena la configuración del daemon (vault path, db path).
type Config struct {
	VaultPath string `json:"vault_path"`
	DBPath    string `json:"db_path"`
}

// Selection es la selección persistida completa.
type Selection struct {
	Version      int                        `json:"version"`
	LastModified time.Time                  `json:"last_modified"`
	Config       Config                     `json:"config"`
	Selected     map[string]ProjectSelection `json:"selected"` // clave: project name
}

// DefaultSelectionPath retorna la ruta default del archivo de selección.
func DefaultSelectionPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".engram", "obsidian-selection.json")
}

// LoadSelection carga la selección desde disco.
// Si el archivo no existe, devuelve una Selection vacía (no error).
func LoadSelection(path string) (*Selection, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Selection{
			Version:  1,
			Selected: make(map[string]ProjectSelection),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Selection
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Selected == nil {
		s.Selected = make(map[string]ProjectSelection)
	}
	return &s, nil
}

// Save persiste la selección a disco.
func (s *Selection) Save(path string) error {
	s.LastModified = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// IsEmpty retorna true si no hay nada seleccionado.
func (s *Selection) IsEmpty() bool {
	return len(s.Selected) == 0
}

// HasConfig retorna true si vault path y db path están configurados.
func (s *Selection) HasConfig() bool {
	return s.Config.VaultPath != "" && s.Config.DBPath != ""
}

// SelectProject marca un proyecto completo como seleccionado.
func (s *Selection) SelectProject(project string) {
	s.Selected[project] = ProjectSelection{Mode: SelectionFull}
}

// DeselectProject quita la selección de un proyecto.
func (s *Selection) DeselectProject(project string) {
	delete(s.Selected, project)
}

// SelectMonth marca un mes completo dentro de un proyecto.
func (s *Selection) SelectMonth(project, month string) {
	ps := s.Selected[project]
	if ps.Months == nil {
		ps.Months = make(map[string]MonthSelection)
	}
	ps.Mode = SelectionPartial
	ps.Months[month] = MonthSelection{Mode: SelectionFull}
	s.Selected[project] = ps
}

// DeselectMonth quita la selección de un mes. Si el proyecto queda sin meses, lo elimina.
func (s *Selection) DeselectMonth(project, month string) {
	ps, ok := s.Selected[project]
	if !ok {
		return
	}
	delete(ps.Months, month)
	if len(ps.Months) == 0 {
		delete(s.Selected, project)
	} else {
		s.Selected[project] = ps
	}
}

// SelectNote marca una nota individual dentro de un mes.
func (s *Selection) SelectNote(project, month string, id int64) {
	ps := s.Selected[project]
	if ps.Months == nil {
		ps.Months = make(map[string]MonthSelection)
	}
	ps.Mode = SelectionPartial
	ms := ps.Months[month]
	ms.Mode = SelectionPartial
	ms.NoteIDs = appendIfMissing(ms.NoteIDs, id)
	ps.Months[month] = ms
	s.Selected[project] = ps
}

// DeselectNote quita una nota individual.
func (s *Selection) DeselectNote(project, month string, id int64) {
	ps, ok := s.Selected[project]
	if !ok {
		return
	}
	ms, ok := ps.Months[month]
	if !ok {
		return
	}
	ms.NoteIDs = removeID(ms.NoteIDs, id)
	if len(ms.NoteIDs) == 0 && ms.Mode == SelectionPartial {
		delete(ps.Months, month)
	} else {
		ps.Months[month] = ms
	}
	if len(ps.Months) == 0 {
		delete(s.Selected, project)
	} else {
		s.Selected[project] = ps
	}
}

// Filter retorna true si la observation debe ser sincronizada según la selección.
// Si la selección está vacía, incluye todo.
func (s *Selection) Filter(obs store.Observation) bool {
	if s.IsEmpty() {
		return true
	}
	ps, ok := s.Selected[obs.ProjectName()]
	if !ok {
		return false
	}
	if ps.Mode == SelectionFull {
		return true
	}
	// Partial: revisar mes
	month := obs.CreatedMonth()
	ms, ok := ps.Months[month]
	if !ok {
		return false
	}
	if ms.Mode == SelectionFull {
		return true
	}
	// Partial: revisar note ID
	for _, id := range ms.NoteIDs {
		if id == obs.ID {
			return true
		}
	}
	return false
}

func appendIfMissing(ids []int64, id int64) []int64 {
	for _, v := range ids {
		if v == id {
			return ids
		}
	}
	return append(ids, id)
}

func removeID(ids []int64, id int64) []int64 {
	out := ids[:0]
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}
