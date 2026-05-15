package obsidian

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

// defaultStateFilePath retorna el path del state file fuera del vault,
// en ~/.engram/obsidian-sync-state.json, para que Cleanup() no lo borre.
func defaultStateFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".engram", "obsidian-sync-state.json")
}

const engramSubdir = "_engram"

// ObsRef referencia ligera a una observation (para índices internos).
type ObsRef struct {
	Slug      string
	Title     string
	TopicKey  string
	Type      string
	Project   string
	CreatedAt string
	RelPath   string // path relativo al vault root (sin .md)
}

// Exporter escribe observaciones filtradas al vault de Obsidian.
type Exporter struct {
	vaultPath string
	logf      func(string, ...any)
}

// NewExporter crea un Exporter.
func NewExporter(vaultPath string, logf func(string, ...any)) *Exporter {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Exporter{vaultPath: vaultPath, logf: logf}
}

// EngramRoot retorna el path absoluto de la carpeta _engram/ dentro del vault.
func (e *Exporter) EngramRoot() string {
	return filepath.Join(e.vaultPath, engramSubdir)
}

// stateFilePath retorna el path del state file fuera del vault.
// Se guarda en ~/.engram/ para que Cleanup() (que borra _engram/) no lo destruya.
func (e *Exporter) stateFilePath() string {
	return defaultStateFilePath()
}

// Export sincroniza las observations filtradas al vault.
// filter es una función que devuelve true para las observations a incluir.
// Si filter es nil, exporta todo.
func (e *Exporter) Export(data *store.ExportData, filter func(store.Observation) bool) (*ExportResult, error) {
	result := &ExportResult{}

	// Leer state incremental
	state, err := ReadState(e.stateFilePath())
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}

	engramRoot := engramSubdir // relativo al vault para wikilinks
	engramAbs := e.EngramRoot()

	if err := os.MkdirAll(engramAbs, 0755); err != nil {
		return nil, fmt.Errorf("mkdir engram root: %w", err)
	}

	// Escribir graph.json
	if err := WriteGraphConfig(e.vaultPath); err != nil {
		e.logf("WARN graph config: %v", err)
	}

	// Procesar observations
	written := map[int64]ObsRef{}

	for _, obs := range data.Observations {
		if obs.IsDeleted() {
			// Eliminar si existía
			if oldPath, ok := state.Files[obs.ID]; ok {
				_ = os.Remove(filepath.Join(e.vaultPath, oldPath+".md"))
				delete(state.Files, obs.ID)
				result.Deleted++
			}
			continue
		}

		if filter != nil && !filter(obs) {
			result.Skipped++
			continue
		}

		relPath := ObservationPath(engramRoot, obs)
		absPath := filepath.Join(e.vaultPath, relPath+".md")

		// Determinar si es create o update.
		// wasKnown=true → ya estaba en state → es un update.
		// wasKnown=false → primera vez → es un create.
		_, wasKnown := state.Files[obs.ID]

		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("mkdir for obs %d: %w", obs.ID, err))
			continue
		}

		md := ObservationToMarkdown(obs, relPath, engramRoot)
		if err := os.WriteFile(absPath, []byte(md), 0644); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("write obs %d: %w", obs.ID, err))
			continue
		}

		state.Files[obs.ID] = relPath
		written[obs.ID] = ObsRef{
			Slug:      filepath.Base(relPath),
			Title:     obs.Title,
			TopicKey:  obs.TopicKeyStr(),
			Type:      obs.Type,
			Project:   obs.ProjectName(),
			CreatedAt: obs.CreatedAt,
			RelPath:   relPath,
		}

		if wasKnown {
			result.Updated++
		} else {
			result.Created++
		}
	}

	// Eliminar archivos que estaban en state pero ya no pasan el filtro.
	// Solo se borran observation notes (IDs en state.Files), nunca índices.
	for id, relPath := range state.Files {
		if _, wasWritten := written[id]; !wasWritten {
			absPath := filepath.Join(e.vaultPath, relPath+".md")
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				e.logf("WARN delete stale obs %d (%s): %v", id, relPath, err)
			} else {
				result.Deleted++
			}
			delete(state.Files, id)
		}
	}

	// Generar índices
	e.writeIndexes(data.Observations, filter, engramRoot, engramAbs)

	// Persistir state
	if err := WriteState(e.stateFilePath(), state); err != nil {
		e.logf("WARN write state: %v", err)
	}

	return result, nil
}

// Cleanup elimina _engram/ y graph.json del vault.
func (e *Exporter) Cleanup() error {
	engramAbs := e.EngramRoot()
	if err := os.RemoveAll(engramAbs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove engram dir: %w", err)
	}
	if err := RemoveGraphConfig(e.vaultPath); err != nil {
		return fmt.Errorf("remove graph config: %w", err)
	}
	return nil
}


// monthDirForObs returns the month directory name (e.g. "05-mayo") for an observation.
// Returns "sin-fecha" if no valid month can be parsed.
func monthDirForObs(obs store.Observation) string {
	if len(obs.CreatedAt) >= 7 {
		m := int(obs.CreatedAt[5]-'0')*10 + int(obs.CreatedAt[6]-'0')
		if m >= 1 && m <= 12 {
			return fmt.Sprintf("%02d-%s", m, monthNames[m-1])
		}
	}
	return "sin-fecha"
}

func (e *Exporter) writeIndexes(obs []store.Observation, filter func(store.Observation) bool, engramRoot, engramAbs string) {
	// Collect filtered observations grouped by project → year → month
	type monthKey struct{ year, month string }
	type projData struct {
		// years → months → observations
		years map[string]map[string][]store.Observation
	}

	projects := map[string]*projData{}

	for _, o := range obs {
		if o.IsDeleted() {
			continue
		}
		if filter != nil && !filter(o) {
			continue
		}
		proj := sanitize(o.ProjectName())
		year := o.CreatedYear()
		if year == "" {
			year = "sin-fecha"
		}
		month := monthDirForObs(o)

		pd, ok := projects[proj]
		if !ok {
			pd = &projData{years: map[string]map[string][]store.Observation{}}
			projects[proj] = pd
		}
		if pd.years[year] == nil {
			pd.years[year] = map[string][]store.Observation{}
		}
		pd.years[year][month] = append(pd.years[year][month], o)
	}

	// ── Root _index.md ─────────────────────────────────────────────────────────
	var rootSb strings.Builder
	rootSb.WriteString("---\ntags: [engram, index]\n---\n\n# Engram Memory Index\n\n## Projects\n\n")
	for proj, pd := range projects {
		total := 0
		for _, months := range pd.years {
			for _, mObs := range months {
				total += len(mObs)
			}
		}
		fmt.Fprintf(&rootSb, "- [[%s|%s]] (%d memorias)\n",
			filepath.Join(engramRoot, proj, "📁 "+proj), proj, total)
	}
	_ = os.WriteFile(filepath.Join(engramAbs, "_index.md"), []byte(rootSb.String()), 0644)

	// ── Per-project, per-year, per-month indexes ────────────────────────────────
	for proj, pd := range projects {
		projDir := filepath.Join(engramAbs, proj)
		if err := os.MkdirAll(projDir, 0755); err != nil {
			e.logf("WARN mkdir project dir %s: %v", projDir, err)
			continue
		}

		// Project index: _engram/{project}/{project}.md
		var projSb strings.Builder
		fmt.Fprintf(&projSb, "---\ntags: [engram, index, project]\n---\n\n# %s\n\n[[%s|← Index]]\n\n", proj, filepath.Join(engramRoot, "_index"))
		for year, months := range pd.years {
			yearTotal := 0
			for _, mObs := range months {
				yearTotal += len(mObs)
			}
			fmt.Fprintf(&projSb, "## %s\n\n", year)
			for month, mObs := range months {
				fmt.Fprintf(&projSb, "- [[%s|%s]] — %d memorias\n",
					filepath.Join(engramRoot, proj, year, month, month), month, len(mObs))
			}
			projSb.WriteString("\n")
			_ = yearTotal // already used for root index
		}
		projFile := filepath.Join(projDir, "📁 "+proj+".md")
		_ = os.WriteFile(projFile, []byte(projSb.String()), 0644)

		// Year and month indexes
		for year, months := range pd.years {
			yearDir := filepath.Join(projDir, year)
			if err := os.MkdirAll(yearDir, 0755); err != nil {
				e.logf("WARN mkdir year dir %s: %v", yearDir, err)
				continue
			}

			// Year index: _engram/{project}/{year}/{year}.md
			var yearSb strings.Builder
			fmt.Fprintf(&yearSb, "---\ntags: [engram, index, year]\n---\n\n# %s / %s\n\n[[%s|← %s]]\n\n",
				proj, year,
				filepath.Join(engramRoot, proj, "📁 "+proj), proj)
			for month, mObs := range months {
				fmt.Fprintf(&yearSb, "## %s\n\n", month)
				fmt.Fprintf(&yearSb, "- [[%s|%s]] — %d memorias\n\n",
					filepath.Join(engramRoot, proj, year, month, month), month, len(mObs))
			}
			yearFile := filepath.Join(yearDir, year+".md")
			_ = os.WriteFile(yearFile, []byte(yearSb.String()), 0644)

			// Month indexes: _engram/{project}/{year}/{month}/{month}.md
			for month, mObs := range months {
				monthDir := filepath.Join(yearDir, month)
				if err := os.MkdirAll(monthDir, 0755); err != nil {
					e.logf("WARN mkdir month dir %s: %v", monthDir, err)
					continue
				}

				var monthSb strings.Builder
				fmt.Fprintf(&monthSb, "---\ntags: [engram, index, month]\n---\n\n# %s / %s / %s\n\n[[%s|← %s]]\n\n",
					proj, year, month,
					filepath.Join(engramRoot, proj, year, year), year)
				for _, o := range mObs {
					obsRelPath := ObservationPath(engramRoot, o)
					fmt.Fprintf(&monthSb, "- [[%s|%s]] — %s\n", obsRelPath, o.Title, sanitize(o.Type))
				}
				monthFile := filepath.Join(monthDir, month+".md")
				_ = os.WriteFile(monthFile, []byte(monthSb.String()), 0644)
			}
		}
	}
}
