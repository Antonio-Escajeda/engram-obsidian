package obsidian

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

var monthNames = []string{
	"enero", "febrero", "marzo", "abril", "mayo", "junio",
	"julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre",
}

// ObservationPath calcula el path relativo al vault (sin .md) para una observation.
func ObservationPath(engramRoot string, obs store.Observation) string {
	project := sanitize(obs.ProjectName())
	obsType := sanitize(obs.Type)
	slug := Slugify(CanonicalExportTitle(obs), obs.ID)
	year := obs.CreatedYear()
	month := ""
	if len(obs.CreatedAt) >= 7 {
		m := int(obs.CreatedAt[5]-'0')*10 + int(obs.CreatedAt[6]-'0')
		if m >= 1 && m <= 12 {
			month = fmt.Sprintf("%02d-%s", m, monthNames[m-1])
		}
	}
	if year == "" {
		year = "sin-fecha"
	}
	if month == "" {
		month = "sin-fecha"
	}
	return filepath.Join(engramRoot, project, year, month, obsType, slug)
}

// ObservationToMarkdown convierte una observation a markdown con frontmatter completo.
// peers es la lista de observations del mismo proyecto+tipo para generar cross-links
// en modo full_mesh. Pasar nil o vacío para modo star (sin sección Relacionadas).
func ObservationToMarkdown(obs store.Observation, relPath string, engramRoot string, peers []store.Observation) string {
	project := sanitize(obs.ProjectName())
	obsType := sanitize(obs.Type)
	topicKey := obs.TopicKeyStr()

	year := obs.CreatedYear()
	month := ""
	monthDir := ""
	if len(obs.CreatedAt) >= 7 {
		m := int(obs.CreatedAt[5]-'0')*10 + int(obs.CreatedAt[6]-'0')
		if m >= 1 && m <= 12 {
			month = fmt.Sprintf("%02d-%s", m, monthNames[m-1])
			monthDir = month
		}
	}
	if year == "" {
		year = "sin-fecha"
	}
	if monthDir == "" {
		monthDir = "sin-fecha"
	}

	created := ""
	if len(obs.CreatedAt) >= 10 {
		created = obs.CreatedAt[:10]
	}
	updated := ""
	if len(obs.UpdatedAt) >= 10 {
		updated = obs.UpdatedAt[:10]
	}

	title := CanonicalExportTitle(obs)
	safeTitle := strings.ReplaceAll(title, `"`, `'`)

	_ = month // usado solo para monthDir

	var sb strings.Builder

	// Frontmatter
	fmt.Fprintf(&sb, "---\n")
	fmt.Fprintf(&sb, "id: %d\n", obs.ID)
	fmt.Fprintf(&sb, "type: %s\n", obsType)
	fmt.Fprintf(&sb, "project: %s\n", project)
	fmt.Fprintf(&sb, "scope: %s\n", obs.Scope)
	fmt.Fprintf(&sb, "topic_key: %s\n", topicKey)
	fmt.Fprintf(&sb, "session_id: %s\n", obs.SessionID)
	fmt.Fprintf(&sb, "revision_count: %d\n", obs.RevisionCount)
	fmt.Fprintf(&sb, "created: %s\n", created)
	fmt.Fprintf(&sb, "updated: %s\n", updated)
	fmt.Fprintf(&sb, "tags: [engram, %s, %s]\n", obsType, project)
	fmt.Fprintf(&sb, "aliases:\n  - \"%s\"\n", safeTitle)
	fmt.Fprintf(&sb, "---\n\n")

	// Breadcrumb: wikilink al padre inmediato + wikilink al hub de tipo
	hubPath := filepath.Join(engramRoot, project, "📋 "+obsType)
	if monthDir != "sin-fecha" {
		fmt.Fprintf(&sb, "> [[%s|%s]] · [[%s|📋 %s]]  \n\n",
			filepath.Join(engramRoot, project, year, monthDir, monthDir), monthDir,
			hubPath, obsType)
	} else if year != "sin-fecha" {
		fmt.Fprintf(&sb, "> [[%s|%s]] · [[%s|📋 %s]]  \n\n",
			filepath.Join(engramRoot, project, year, year), year,
			hubPath, obsType)
	} else {
		fmt.Fprintf(&sb, "> [[%s|%s]] · [[%s|📋 %s]]  \n\n",
			filepath.Join(engramRoot, project, "📁 "+project), project,
			hubPath, obsType)
	}

	// Contenido
	fmt.Fprintf(&sb, "# %s\n\n%s\n", title, obs.Content)

	// Sección Relacionadas — solo en modo full_mesh (cuando peers no está vacío).
	if len(peers) > 0 {
		fmt.Fprintf(&sb, "\n---\n\n## Relacionadas (%s)\n\n", obsType)
		for _, peer := range peers {
			peerPath := ObservationPath(engramRoot, peer)
			fmt.Fprintf(&sb, "- [[%s|%s]]\n", peerPath, CanonicalExportTitle(peer))
		}
	}

	return sb.String()
}

var invalidChars = strings.NewReplacer(
	"<", "-", ">", "-", ":", "-", `"`, "-",
	"/", "-", `\`, "-", "|", "-", "?", "-", "*", "-",
)

func sanitize(s string) string {
	r := invalidChars.Replace(s)
	if len(r) > 100 {
		r = r[:100]
	}
	return strings.TrimSpace(r)
}
