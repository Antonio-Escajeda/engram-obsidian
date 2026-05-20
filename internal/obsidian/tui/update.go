package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Update implementa tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg.String())
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil
	}
	// Propagar a inputs si estamos en config
	if m.Screen == ScreenConfig {
		return m.updateConfigInputs(msg)
	}
	return m, nil
}

func (m Model) handleKey(key string) (tea.Model, tea.Cmd) {
	switch m.Screen {
	case ScreenConfig:
		return m.handleConfigKey(key)
	case ScreenSelection:
		return m.handleSelectionKey(key)
	}
	return m, nil
}

// ── Config screen ─────────────────────────────────────────────────────────────

func (m Model) handleConfigKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c", "q":
		m.Quit = true
		return m, tea.Quit

	case "tab", "down":
		m.ConfigFocus = (m.ConfigFocus + 1) % 4
		return m.updateConfigFocus()

	case "shift+tab", "up":
		m.ConfigFocus = (m.ConfigFocus + 3) % 4
		return m.updateConfigFocus()

	case "b":
		// Browse solo disponible cuando vault_path está enfocado
		if m.ConfigFocus == 0 {
			wslPath, err := browseWindowsFolder()
			if err != nil {
				m.StatusMsg = "Browse: " + err.Error()
				return m, nil
			}
			m.VaultInput.SetValue(wslPath)
			// Validar que sea un path /mnt/* (accesible desde Windows)
			if !strings.HasPrefix(wslPath, "/mnt/") {
				m.StatusMsg = "Advertencia: el path no empieza con /mnt/ — puede no ser accesible desde Windows"
			} else if vaultHasEngramDir(wslPath) {
				m.StatusMsg = "Vault existente detectado (_engram/ encontrado)"
			} else {
				m.StatusMsg = ""
			}
			return m, nil
		}

	case "left", "h":
		if m.ConfigFocus == 2 {
			m.GraphMode = "star"
			return m, nil
		}

	case "right", "l":
		if m.ConfigFocus == 2 {
			m.GraphMode = "full_mesh"
			return m, nil
		}

	case " ":
		if m.ConfigFocus == 2 {
			if m.GraphMode == "full_mesh" {
				m.GraphMode = "star"
			} else {
				m.GraphMode = "full_mesh"
			}
			return m, nil
		}

	case "enter":
		if m.ConfigFocus == 2 {
			// Toggle graph mode con enter cuando está enfocado
			if m.GraphMode == "full_mesh" {
				m.GraphMode = "star"
			} else {
				m.GraphMode = "full_mesh"
			}
			return m, nil
		}
		if m.ConfigFocus == 3 {
			// Confirmar config
			vault := expandHome(m.VaultInput.Value())
			db := expandHome(m.DBInput.Value())
			if vault == "" || db == "" {
				m.StatusMsg = "Vault path y DB path son requeridos"
				return m, nil
			}
			if !strings.HasPrefix(vault, "/mnt/") {
				m.StatusMsg = "Advertencia: vault path no empieza con /mnt/ — puede no ser accesible desde Windows"
				return m, nil
			}
			m.Selection.Config.VaultPath = vault
			m.Selection.Config.DBPath = contractHome(expandHome(db))
			m.Selection.Config.GraphMode = m.GraphMode
			m.StatusMsg = ""
			m.Screen = ScreenSelection
			m.Flat = FlatNodes(m.Roots)
			m.Cursor = 0
			return m, nil
		}
		// Enter en un input: avanzar foco
		m.ConfigFocus = (m.ConfigFocus + 1) % 4
		return m.updateConfigFocus()
	}

	// Delegar tecla al input activo
	return m.updateConfigInputs(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

func (m Model) updateConfigFocus() (tea.Model, tea.Cmd) {
	switch m.ConfigFocus {
	case 0:
		m.VaultInput.Focus()
		m.DBInput.Blur()
	case 1:
		m.VaultInput.Blur()
		m.DBInput.Focus()
	case 2, 3:
		m.VaultInput.Blur()
		m.DBInput.Blur()
	}
	return m, textinput.Blink
}

func (m Model) updateConfigInputs(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	if m.ConfigFocus == 0 {
		m.VaultInput, cmd = m.VaultInput.Update(msg)
		cmds = append(cmds, cmd)
	} else if m.ConfigFocus == 1 {
		m.DBInput, cmd = m.DBInput.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// ── Selection screen ──────────────────────────────────────────────────────────

func (m Model) handleSelectionKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		m.Quit = true
		return m, tea.Quit

	case "q":
		// q sin ctrl vuelve a config
		m.Screen = ScreenConfig
		m.Cursor = 0
		m.Scroll = 0
		return m, nil

	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			if m.Cursor < m.Scroll {
				m.Scroll = m.Cursor
			}
		}

	case "down", "j":
		if m.Cursor < len(m.Flat)-1 {
			m.Cursor++
			visible := m.visibleItems()
			if m.Cursor >= m.Scroll+visible {
				m.Scroll = m.Cursor - visible + 1
			}
		}

	case " ":
		if m.Cursor < len(m.Flat) {
			Toggle(m.Flat[m.Cursor], m.Roots)
			m.Flat = FlatNodes(m.Roots)
		}

	case "enter", "right", "l":
		// Expandir/colapsar proyecto o mes
		if m.Cursor < len(m.Flat) {
			node := m.Flat[m.Cursor]
			if node.Kind == NodeProject || node.Kind == NodeMonth {
				node.Expanded = !node.Expanded
				m.Flat = FlatNodes(m.Roots)
			}
		}

	case "left", "h":
		// Colapsar
		if m.Cursor < len(m.Flat) {
			node := m.Flat[m.Cursor]
			if node.Kind == NodeProject || node.Kind == NodeMonth {
				node.Expanded = false
				m.Flat = FlatNodes(m.Roots)
			}
		}

	case "a":
		// Seleccionar todo
		for _, proj := range m.Roots {
			proj.Check = CheckFull
			setAllChildren(proj, CheckFull)
		}
		m.Flat = FlatNodes(m.Roots)

	case "n":
		// Deseleccionar todo
		for _, proj := range m.Roots {
			proj.Check = CheckNone
			setAllChildren(proj, CheckNone)
		}
		m.Flat = FlatNodes(m.Roots)

	case "s", "ctrl+s":
		// Confirmar y sincronizar
		m.Selection = ToSelection(m.Roots, m.Selection)
		m.Confirmed = true
		return m, tea.Quit
	}

	return m, nil
}

func (m Model) visibleItems() int {
	v := m.Height - 8 // header + footer
	if v < 5 {
		v = 5
	}
	return v
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func contractHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home+"/") {
		return "~/" + path[len(home)+1:]
	}
	return path
}
