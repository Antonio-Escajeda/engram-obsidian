package obsidian

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

// removeEmptyDirs elimina recursivamente directorios vacíos dentro de root.
// No elimina root en sí mismo.
func removeEmptyDirs(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(root, e.Name())
		removeEmptyDirs(sub)
		// Intentar borrar — solo tiene efecto si quedó vacío.
		_ = os.Remove(sub)
	}
}

// removeStaleMonthDirs elimina directorios de mes (y año) dentro de _engram/ que ya no
// tienen observaciones activas según activeMonths.
// activeMonths usa keys del estilo "proj/2026/04-abril".
func (e *Exporter) removeStaleMonthDirs(engramAbs string, activeMonths map[string]bool) {
	projEntries, err := os.ReadDir(engramAbs)
	if err != nil {
		return
	}
	for _, projEntry := range projEntries {
		if !projEntry.IsDir() {
			continue
		}
		projDir := filepath.Join(engramAbs, projEntry.Name())
		yearEntries, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}
		for _, yearEntry := range yearEntries {
			if !yearEntry.IsDir() {
				continue
			}
			yearDir := filepath.Join(projDir, yearEntry.Name())
			monthEntries, err := os.ReadDir(yearDir)
			if err != nil {
				continue
			}
			for _, monthEntry := range monthEntries {
				if !monthEntry.IsDir() {
					continue
				}
				key := filepath.Join(projEntry.Name(), yearEntry.Name(), monthEntry.Name())
				if !activeMonths[key] {
					staleDir := filepath.Join(yearDir, monthEntry.Name())
					if err := os.RemoveAll(staleDir); err != nil {
						e.logf("WARN delete stale month dir %s: %v", staleDir, err)
					} else {
						e.logf("Removed stale month dir: %s", key)
					}
				}
			}
			// Si el año quedó vacío de meses (solo puede tener el año.md), borrarlo.
			// removeEmptyDirs se encarga luego, pero borramos el year.md explícitamente
			// para que el directorio quede vacío y removeEmptyDirs lo elimine.
			remaining, _ := os.ReadDir(yearDir)
			allIndexFiles := true
			for _, r := range remaining {
				if r.IsDir() {
					allIndexFiles = false
					break
				}
			}
			if allIndexFiles && len(remaining) > 0 {
				// Solo quedan archivos (ej: {year}.md) — verificar si hay meses activos en este año.
				hasActiveMonth := false
				prefix := filepath.Join(projEntry.Name(), yearEntry.Name()) + string(filepath.Separator)
				for k := range activeMonths {
					if strings.HasPrefix(k, prefix) {
						hasActiveMonth = true
						break
					}
				}
				if !hasActiveMonth {
					if err := os.RemoveAll(yearDir); err != nil {
						e.logf("WARN delete stale year dir %s: %v", yearDir, err)
					} else {
						e.logf("Removed stale year dir: %s/%s", projEntry.Name(), yearEntry.Name())
					}
				}
			}
		}
	}
}

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
	graphMode string
	logf      func(string, ...any)
}

// NewExporter crea un Exporter.
func NewExporter(vaultPath string, graphMode string, logf func(string, ...any)) *Exporter {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if graphMode == "" {
		graphMode = "star"
	}
	return &Exporter{vaultPath: vaultPath, graphMode: graphMode, logf: logf}
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

// workspaceStateFilePath retorna el path donde se guarda el lastOpenFiles del vault.
func (e *Exporter) workspaceStateFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".engram", "obsidian-workspace-state.json")
}

// saveWorkspaceState lee lastOpenFiles de .obsidian/workspace.json y, si tiene
// entradas, las persiste en ~/.engram/obsidian-workspace-state.json; luego limpia
// el campo en el archivo para no revelar qué memorias estaba viendo el usuario.
func (e *Exporter) saveWorkspaceState() error {
	workspacePath := filepath.Join(e.vaultPath, ".obsidian", "workspace.json")
	raw, err := os.ReadFile(workspacePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no hay workspace.json — no es error
		}
		return fmt.Errorf("read workspace.json: %w", err)
	}

	// Usar map[string]json.RawMessage para no perder campos desconocidos.
	var ws map[string]json.RawMessage
	if err := json.Unmarshal(raw, &ws); err != nil {
		return fmt.Errorf("parse workspace.json: %w", err)
	}

	rawFiles, ok := ws["lastOpenFiles"]
	if !ok {
		return nil // campo ausente — nada que hacer
	}

	var lastOpenFiles []string
	if err := json.Unmarshal(rawFiles, &lastOpenFiles); err != nil {
		return fmt.Errorf("parse lastOpenFiles: %w", err)
	}

	if len(lastOpenFiles) > 0 {
		// Guardar el array en el state file.
		stateData, err := json.Marshal(lastOpenFiles)
		if err != nil {
			return fmt.Errorf("marshal workspace state: %w", err)
		}
		if err := os.WriteFile(e.workspaceStateFilePath(), stateData, 0600); err != nil {
			return fmt.Errorf("write workspace state: %w", err)
		}
	}

	// Limpiar lastOpenFiles en el workspace.json.
	ws["lastOpenFiles"] = json.RawMessage(`[]`)
	out, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace.json: %w", err)
	}
	if err := os.WriteFile(workspacePath, out, 0644); err != nil {
		return fmt.Errorf("write workspace.json: %w", err)
	}

	return nil
}

// restoreWorkspaceState lee ~/.engram/obsidian-workspace-state.json y, si existe,
// restaura lastOpenFiles en .obsidian/workspace.json; luego borra el state file.
func (e *Exporter) restoreWorkspaceState() error {
	stateFile := e.workspaceStateFilePath()
	stateData, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no hay estado guardado — no es error
		}
		return fmt.Errorf("read workspace state: %w", err)
	}

	var lastOpenFiles []string
	if err := json.Unmarshal(stateData, &lastOpenFiles); err != nil {
		return fmt.Errorf("parse workspace state: %w", err)
	}

	workspacePath := filepath.Join(e.vaultPath, ".obsidian", "workspace.json")
	raw, err := os.ReadFile(workspacePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No hay workspace.json — borrar el state file de todas formas y salir.
			_ = os.Remove(stateFile)
			return nil
		}
		return fmt.Errorf("read workspace.json: %w", err)
	}

	var ws map[string]json.RawMessage
	if err := json.Unmarshal(raw, &ws); err != nil {
		return fmt.Errorf("parse workspace.json: %w", err)
	}

	restored, err := json.Marshal(lastOpenFiles)
	if err != nil {
		return fmt.Errorf("marshal lastOpenFiles: %w", err)
	}
	ws["lastOpenFiles"] = json.RawMessage(restored)

	out, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace.json: %w", err)
	}
	if err := os.WriteFile(workspacePath, out, 0644); err != nil {
		return fmt.Errorf("write workspace.json: %w", err)
	}

	// Consumir el state file.
	_ = os.Remove(stateFile)

	return nil
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

	// Para full_mesh: construir índice por proyecto+tipo para obtener peers rápido.
	// typeIndex[project][type] → lista de observations activas y filtradas.
	var typeIndex map[string]map[string][]store.Observation
	if e.graphMode == "full_mesh" {
		typeIndex = map[string]map[string][]store.Observation{}
		for _, obs := range data.Observations {
			if obs.IsDeleted() {
				continue
			}
			if filter != nil && !filter(obs) {
				continue
			}
			proj := obs.ProjectName()
			obsType := obs.Type
			if typeIndex[proj] == nil {
				typeIndex[proj] = map[string][]store.Observation{}
			}
			typeIndex[proj][obsType] = append(typeIndex[proj][obsType], obs)
		}
	}

	// Procesar observations
	written := map[int64]ObsRef{}
	activeMonths := map[string]bool{}

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

		// Obtener peers para full_mesh: mismas tipo+proyecto, excluyendo la nota actual.
		var peers []store.Observation
		if e.graphMode == "full_mesh" {
			for _, candidate := range typeIndex[obs.ProjectName()][obs.Type] {
				if candidate.ID != obs.ID {
					peers = append(peers, candidate)
				}
			}
		}

		md := ObservationToMarkdown(obs, relPath, engramRoot, peers)
		if err := os.WriteFile(absPath, []byte(md), 0644); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("write obs %d: %w", obs.ID, err))
			continue
		}

		state.Files[obs.ID] = relPath

		// Acumular mes activo para cleanup posterior de índices de mes/año huérfanos.
		{
			year := obs.CreatedYear()
			if year == "" {
				year = "sin-fecha"
			}
			month := monthDirForObs(obs)
			activeMonths[filepath.Join(sanitize(obs.ProjectName()), year, month)] = true
		}

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

	// Generar índices solo para los proyectos activos.
	e.writeIndexes(data.Observations, filter, engramRoot, engramAbs)

	// Eliminar directorios de mes/año que ya no tienen observaciones activas.
	e.removeStaleMonthDirs(engramAbs, activeMonths)

	// Sweep de índices: eliminar subdirectorios de proyectos que ya no están activos.
	// Los archivos de observaciones individuales ya fueron borrados arriba (sweep de state.Files).
	// Pero los directorios de proyecto (_engram/{proj}/) y sus índices internos
	// nunca están en state.Files, así que hay que barrerlos por separado.
	activeProjects := map[string]bool{}
	for _, obs := range data.Observations {
		if obs.IsDeleted() {
			continue
		}
		if filter != nil && !filter(obs) {
			continue
		}
		activeProjects[sanitize(obs.ProjectName())] = true
	}
	if entries, err := os.ReadDir(engramAbs); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if !activeProjects[entry.Name()] {
				staleDir := filepath.Join(engramAbs, entry.Name())
				if err := os.RemoveAll(staleDir); err != nil {
					e.logf("WARN delete stale project dir %s: %v", staleDir, err)
				} else {
					e.logf("Removed stale project dir: %s", entry.Name())
				}
			}
		}
	}

	// Eliminar directorios vacíos que hayan quedado luego del sweep.
	removeEmptyDirs(engramAbs)

	// Persistir state
	if err := WriteState(e.stateFilePath(), state); err != nil {
		e.logf("WARN write state: %v", err)
	}

	// Restaurar lastOpenFiles en .obsidian/workspace.json si hay un estado guardado.
	if err := e.restoreWorkspaceState(); err != nil {
		e.logf("WARN restore workspace state: %v", err)
	}

	return result, nil
}

// Cleanup elimina _engram/ y graph.json del vault.
// Antes de borrar, guarda lastOpenFiles de .obsidian/workspace.json para que
// restoreWorkspaceState() pueda recuperarlos en el próximo Export().
func (e *Exporter) Cleanup() error {
	if err := e.saveWorkspaceState(); err != nil {
		e.logf("WARN save workspace state: %v", err)
	}
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
		// types → observations (para hubs por tipo)
		types map[string][]store.Observation
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
			pd = &projData{
				years: map[string]map[string][]store.Observation{},
				types: map[string][]store.Observation{},
			}
			projects[proj] = pd
		}
		if pd.years[year] == nil {
			pd.years[year] = map[string][]store.Observation{}
		}
		pd.years[year][month] = append(pd.years[year][month], o)

		obsType := sanitize(o.Type)
		pd.types[obsType] = append(pd.types[obsType], o)
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

		// ── Hub files por tipo: _engram/{project}/📋 {type}.md ───────────────────
		for obsType, typeObs := range pd.types {
			var hubSb strings.Builder
			fmt.Fprintf(&hubSb, "---\ntags: [engram-hub, %s]\n---\n\n# %s / %s\n\n[[%s|← %s]]\n\n",
				obsType, proj, obsType,
				filepath.Join(engramRoot, proj, "📁 "+proj), proj)
			fmt.Fprintf(&hubSb, "## Memorias (%d)\n\n", len(typeObs))
			for _, o := range typeObs {
				obsRelPath := ObservationPath(engramRoot, o)
				fmt.Fprintf(&hubSb, "- [[%s|%s]]\n", obsRelPath, o.Title)
			}
			hubFile := filepath.Join(projDir, "📋 "+obsType+".md")
			_ = os.WriteFile(hubFile, []byte(hubSb.String()), 0644)
		}
	}
}
