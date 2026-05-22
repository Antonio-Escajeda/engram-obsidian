package obsidian

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

var canonicalTitlePrefixPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \[[^\]]+\] - `)

// CanonicalExportTitle normaliza el titulo solo para exportacion markdown.
// Formato canonico: YYYY-MM-DD [tipo] - titulo
func CanonicalExportTitle(obs store.Observation) string {
	rawTitle := strings.TrimSpace(obs.Title)
	if canonicalTitlePrefixPattern.MatchString(rawTitle) {
		return rawTitle
	}

	date := canonicalDate(obs.CreatedAt)
	obsType := canonicalType(obs.Type)
	base := canonicalBaseTitle(rawTitle, obs.ID)

	return fmt.Sprintf("%s [%s] - %s", date, obsType, base)
}

func canonicalDate(createdAt string) string {
	if len(createdAt) >= 10 {
		return createdAt[:10]
	}
	return "0000-00-00"
}

func canonicalType(obsType string) string {
	t := sanitize(strings.TrimSpace(obsType))
	if t == "" {
		return "manual"
	}
	return t
}

func canonicalBaseTitle(rawTitle string, id int64) string {
	if rawTitle != "" {
		return rawTitle
	}
	return fmt.Sprintf("observation-%d", id)
}
