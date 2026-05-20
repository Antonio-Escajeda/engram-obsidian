package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	styleSubtitle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	styleCursor   = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	styleCheck    = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	stylePartial  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleInput    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleHint     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleFocused  = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
)

// View implementa tea.Model.
func (m Model) View() string {
	switch m.Screen {
	case ScreenConfig:
		return m.viewConfig()
	case ScreenSelection:
		return m.viewSelection()
	}
	return ""
}

func (m Model) viewConfig() string {
	var sb strings.Builder

	sb.WriteString(styleTitle.Render("engram-obsidian") + "\n")
	sb.WriteString(styleSubtitle.Render("Configuración inicial") + "\n\n")

	// Vault input
	label := "  Vault path: "
	if m.ConfigFocus == 0 {
		label = styleFocused.Render("▶ Vault path: ")
	}
	sb.WriteString(label + m.VaultInput.View() + "\n")

	// Hint de browse — solo visible cuando vault_path está enfocado
	if m.ConfigFocus == 0 {
		sb.WriteString(styleHint.Render("  Presioná 'b' para explorar (selector de carpetas de Windows)") + "\n")
	}
	sb.WriteString("\n")

	// DB input
	label = "  DB path:    "
	if m.ConfigFocus == 1 {
		label = styleFocused.Render("▶ DB path:    ")
	}
	sb.WriteString(label + m.DBInput.View() + "\n\n")

	// Graph mode selector
	starRadio := "○ Star"
	meshRadio := "○ Full Mesh"
	if m.GraphMode == "full_mesh" {
		meshRadio = "● Full Mesh"
	} else {
		starRadio = "● Star"
	}
	graphLine := fmt.Sprintf("  Graph mode:  %s   %s", starRadio, meshRadio)
	if m.ConfigFocus == 2 {
		graphLine = styleFocused.Render("▶ Graph mode: ") + fmt.Sprintf(" %s   %s", starRadio, meshRadio)
		graphLine += "   " + styleHint.Render("(← → para cambiar)")
	}
	sb.WriteString(graphLine + "\n\n")

	// Botón confirmar
	btn := "  [ Continuar ]"
	if m.ConfigFocus == 3 {
		btn = styleFocused.Render("▶ [ Continuar ]")
	}
	sb.WriteString(btn + "\n\n")

	if m.StatusMsg != "" {
		sb.WriteString(styleError.Render("  "+m.StatusMsg) + "\n\n")
	}

	sb.WriteString(styleHint.Render("  tab/↓ navegar  •  enter confirmar campo  •  q salir"))
	return sb.String()
}

func (m Model) viewSelection() string {
	var sb strings.Builder

	// Header
	sb.WriteString(styleTitle.Render("engram-obsidian") + "\n")
	sb.WriteString(styleSubtitle.Render(fmt.Sprintf("Seleccioná qué sincronizar  •  vault: %s", truncate(m.Selection.Config.VaultPath, 40))) + "\n\n")

	visible := m.visibleItems()
	end := m.Scroll + visible
	if end > len(m.Flat) {
		end = len(m.Flat)
	}

	for i := m.Scroll; i < end; i++ {
		node := m.Flat[i]
		line := m.renderNode(node, i == m.Cursor)
		sb.WriteString(line + "\n")
	}

	// Padding si hay menos items que el área visible
	for i := end - m.Scroll; i < visible; i++ {
		sb.WriteString("\n")
	}

	// Scroll indicator
	if len(m.Flat) > visible {
		pct := 0
		if len(m.Flat) > 1 {
			pct = (m.Scroll * 100) / (len(m.Flat) - visible)
		}
		sb.WriteString(styleHint.Render(fmt.Sprintf("  %d/%d items  %d%%", m.Cursor+1, len(m.Flat), pct)) + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(styleHint.Render("  ↑↓/jk navegar  •  enter/→ expandir  •  space seleccionar  •  a todo  •  n nada  •  s sincronizar  •  q config"))

	return sb.String()
}

func (m Model) renderNode(node *TreeNode, isCursor bool) string {
	// Indentación por nivel
	indent := ""
	switch node.Kind {
	case NodeMonth:
		indent = "  "
	case NodeNote:
		indent = "    "
	}

	// Checkbox
	checkStr := ""
	switch node.Check {
	case CheckFull:
		checkStr = styleCheck.Render("[x]")
	case CheckPartial:
		checkStr = stylePartial.Render("[-]")
	case CheckNone:
		checkStr = "[ ]"
	}

	// Expand indicator para proyectos y meses
	expand := " "
	if node.Kind == NodeProject || node.Kind == NodeMonth {
		if node.Expanded {
			expand = "▾"
		} else {
			expand = "▸"
		}
	}

	line := fmt.Sprintf("%s%s %s %s", indent, expand, checkStr, node.Label)

	if isCursor {
		// Pad a ancho completo para highlight
		if m.Width > 0 && len(line) < m.Width {
			line = line + strings.Repeat(" ", m.Width-len(line))
		}
		return styleCursor.Render(line)
	}

	if node.Check == CheckFull {
		return styleSelected.Render(line)
	}
	return styleInput.Render(line)
}
