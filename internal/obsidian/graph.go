package obsidian

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type colorGroup struct {
	Query string `json:"query"`
	Color struct {
		A   float64 `json:"a"`
		RGB int     `json:"rgb"`
	} `json:"color"`
}

func newColorGroup(query string, hexRGB string) colorGroup {
	var rgb int
	for _, c := range hexRGB {
		rgb <<= 4
		switch {
		case c >= '0' && c <= '9':
			rgb |= int(c - '0')
		case c >= 'a' && c <= 'f':
			rgb |= int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			rgb |= int(c-'A') + 10
		}
	}
	cg := colorGroup{Query: query}
	cg.Color.A = 1
	cg.Color.RGB = rgb
	return cg
}

// WriteGraphConfig escribe el graph.json de Obsidian con los colores de engram-brain.
func WriteGraphConfig(vaultPath string) error {
	obsidianDir := filepath.Join(vaultPath, ".obsidian")
	if err := os.MkdirAll(obsidianDir, 0755); err != nil {
		return err
	}

	config := map[string]any{
		"collapse-filter":       true,
		"search":                "",
		"showTags":              false,
		"showAttachments":       false,
		"hideUnresolved":        false,
		"showOrphans":           true,
		"collapse-color-groups": false,
		"colorGroups": []colorGroup{
			newColorGroup("tag:#architecture",    "001EFF"),
			newColorGroup("tag:#bugfix",          "FF0000"),
			newColorGroup("tag:#decision",        "00FF2A"),
			newColorGroup("tag:#pattern",         "FF6800"),
			newColorGroup("tag:#discovery",       "9B59B6"),
			newColorGroup("tag:#config",          "F1C40F"),
			newColorGroup("tag:#preference",      "E91E8C"),
			newColorGroup("tag:#session_summary", "17A589"),
		},
		"collapse-display":   true,
		"showArrow":          false,
		"textFadeMultiplier": 0,
		"nodeSizeMultiplier": 1,
		"lineSizeMultiplier": 1,
		"collapse-forces":    true,
		"centerStrength":     0.515,
		"repelStrength":      12.71,
		"linkStrength":       0.729,
		"linkDistance":       207,
		"scale":              1,
		"close":              false,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(obsidianDir, "graph.json"), data, 0644)
}

// RemoveGraphConfig elimina el graph.json del vault.
func RemoveGraphConfig(vaultPath string) error {
	path := filepath.Join(vaultPath, ".obsidian", "graph.json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
