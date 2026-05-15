package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

// Screen identifica la pantalla activa.
type Screen int

const (
	ScreenConfig    Screen = iota // Configuración: vault path + db path
	ScreenSelection               // Árbol de selección de proyectos/meses/notas
)

// Model es el modelo Bubbletea de la TUI.
type Model struct {
	Screen  Screen
	Width   int
	Height  int

	// Config screen
	VaultInput  textinput.Model
	DBInput     textinput.Model
	ConfigFocus int // 0 = vault, 1 = db, 2 = confirmar

	// Selection screen
	Roots  []*TreeNode
	Flat   []*TreeNode // nodos visibles (cache de FlatNodes)
	Cursor int         // índice en Flat
	Scroll int         // offset de scroll

	// Estado
	Selection    *obsidian.Selection
	Observations []store.Observation
	Confirmed    bool
	Quit         bool
	StatusMsg    string
}

// New crea un Model inicializado con la selección y observaciones dadas.
func New(sel *obsidian.Selection, observations []store.Observation) Model {
	vaultInput := textinput.New()
	vaultInput.Placeholder = "~/Obsidian/engram"
	vaultInput.CharLimit = 256

	dbInput := textinput.New()
	dbInput.Placeholder = "~/.engram/engram.db"
	dbInput.CharLimit = 256

	// Si ya hay config, pre-llenar
	if sel.Config.VaultPath != "" {
		vaultInput.SetValue(sel.Config.VaultPath)
	}
	if sel.Config.DBPath != "" {
		dbInput.SetValue(sel.Config.DBPath)
	}

	// Determinar pantalla inicial
	screen := ScreenConfig
	if sel.HasConfig() {
		screen = ScreenSelection
	}

	// Si vault input es la pantalla inicial, enfocarlo
	if screen == ScreenConfig {
		vaultInput.Focus()
	}

	roots := BuildTree(observations, sel)
	flat := FlatNodes(roots)

	return Model{
		Screen:       screen,
		VaultInput:   vaultInput,
		DBInput:      dbInput,
		Roots:        roots,
		Flat:         flat,
		Selection:    sel,
		Observations: observations,
	}
}

// Init implementa tea.Model.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}
