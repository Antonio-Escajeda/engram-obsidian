package tui

import (
	"fmt"
	"sort"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

// NodeKind clasifica el tipo de nodo en el árbol.
type NodeKind int

const (
	NodeProject NodeKind = iota
	NodeMonth
	NodeNote
)

// CheckState representa el estado del checkbox de un nodo.
type CheckState int

const (
	CheckNone    CheckState = iota // [ ]
	CheckPartial                   // [-]
	CheckFull                      // [x]
)

// TreeNode es un nodo del árbol de selección.
type TreeNode struct {
	Kind     NodeKind
	Label    string
	Key      string // project name, "YYYY-MM", o fmt.Sprint(obs.ID)
	ObsID    int64  // solo para NodeNote
	Children []*TreeNode
	Check    CheckState
	Expanded bool
}

// BuildTree construye el árbol desde observaciones, proyectos de DB y selección actual.
func BuildTree(observations []store.Observation, sel *obsidian.Selection, dbProjects []string) []*TreeNode {
	// Agrupar: project → month → []obs
	byProjectMonth := map[string]map[string][]store.Observation{}

	for _, obs := range observations {
		if obs.IsDeleted() {
			continue
		}
		p := obs.ProjectName()
		m := obs.CreatedMonth()
		if m == "" {
			m = "sin-fecha"
		}
		if byProjectMonth[p] == nil {
			byProjectMonth[p] = map[string][]store.Observation{}
		}
		byProjectMonth[p][m] = append(byProjectMonth[p][m], obs)
	}

	projectSet := make(map[string]struct{}, len(byProjectMonth)+len(dbProjects)+len(sel.Selected))
	projects := make([]string, 0, len(byProjectMonth)+len(dbProjects)+len(sel.Selected))
	for p := range byProjectMonth {
		projectSet[p] = struct{}{}
	}
	for _, p := range dbProjects {
		if p == "" {
			continue
		}
		projectSet[p] = struct{}{}
	}
	for p := range sel.Selected {
		if p == "" {
			continue
		}
		projectSet[p] = struct{}{}
	}
	for p := range projectSet {
		projects = append(projects, p)
	}
	sort.Strings(projects)

	var roots []*TreeNode
	for _, proj := range projects {
		months := byProjectMonth[proj]
		if months == nil {
			months = map[string][]store.Observation{}
		}
		monthKeys := make([]string, 0, len(months))
		for m := range months {
			monthKeys = append(monthKeys, m)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(monthKeys)))

		projNode := &TreeNode{
			Kind:  NodeProject,
			Label: fmt.Sprintf("%s (%d obs)", proj, countObs(months)),
			Key:   proj,
		}

		for _, month := range monthKeys {
			obs := months[month]
			monthNode := &TreeNode{
				Kind:  NodeMonth,
				Label: fmt.Sprintf("%s (%d)", month, len(obs)),
				Key:   month,
			}

			for _, o := range obs {
				obsType := o.Type
				if obsType == "" {
					obsType = "unknown"
				}
				noteNode := &TreeNode{
					Kind:  NodeNote,
					Label: fmt.Sprintf("[%s] %s", obsType, truncate(o.Title, 50)),
					Key:   fmt.Sprintf("%d", o.ID),
					ObsID: o.ID,
				}
				monthNode.Children = append(monthNode.Children, noteNode)
			}
			projNode.Children = append(projNode.Children, monthNode)
		}
		roots = append(roots, projNode)
	}

	// Aplicar selección guardada
	applySelection(roots, sel)
	return roots
}

func applySelection(nodes []*TreeNode, sel *obsidian.Selection) {
	for _, proj := range nodes {
		ps, ok := sel.Selected[proj.Key]
		if !ok {
			proj.Check = CheckNone
			continue
		}
		if ps.Mode == obsidian.SelectionFull {
			proj.Check = CheckFull
			setAllChildren(proj, CheckFull)
			continue
		}
		// Partial
		for _, month := range proj.Children {
			ms, ok := ps.Months[month.Key]
			if !ok {
				month.Check = CheckNone
				continue
			}
			if ms.Mode == obsidian.SelectionFull {
				month.Check = CheckFull
				setAllChildren(month, CheckFull)
			} else {
				// Partial notes
				noteSet := map[int64]bool{}
				for _, id := range ms.NoteIDs {
					noteSet[id] = true
				}
				for _, note := range month.Children {
					if noteSet[note.ObsID] {
						note.Check = CheckFull
					} else {
						note.Check = CheckNone
					}
				}
				month.Check = computeParentCheck(month.Children)
			}
		}
		proj.Check = computeParentCheck(proj.Children)
	}
}

func setAllChildren(node *TreeNode, state CheckState) {
	for _, child := range node.Children {
		child.Check = state
		setAllChildren(child, state)
	}
}

func computeParentCheck(children []*TreeNode) CheckState {
	if len(children) == 0 {
		return CheckNone
	}
	full, none := 0, 0
	for _, c := range children {
		switch c.Check {
		case CheckFull:
			full++
		case CheckNone:
			none++
		}
	}
	if full == len(children) {
		return CheckFull
	}
	if none == len(children) {
		return CheckNone
	}
	return CheckPartial
}

// ToSelection convierte el árbol de vuelta a una Selection.
func ToSelection(nodes []*TreeNode, current *obsidian.Selection) *obsidian.Selection {
	out := &obsidian.Selection{
		Version:  1,
		Config:   current.Config,
		Selected: make(map[string]obsidian.ProjectSelection),
	}
	for _, proj := range nodes {
		if len(proj.Children) == 0 && proj.Check == CheckNone {
			if original, ok := current.Selected[proj.Key]; ok {
				out.Selected[proj.Key] = original
			}
			continue
		}
		switch proj.Check {
		case CheckNone:
			continue
		case CheckFull:
			out.Selected[proj.Key] = obsidian.ProjectSelection{Mode: obsidian.SelectionFull}
		case CheckPartial:
			ps := obsidian.ProjectSelection{
				Mode:   obsidian.SelectionPartial,
				Months: make(map[string]obsidian.MonthSelection),
			}
			for _, month := range proj.Children {
				switch month.Check {
				case CheckNone:
					continue
				case CheckFull:
					ps.Months[month.Key] = obsidian.MonthSelection{Mode: obsidian.SelectionFull}
				case CheckPartial:
					ms := obsidian.MonthSelection{Mode: obsidian.SelectionPartial}
					for _, note := range month.Children {
						if note.Check == CheckFull {
							ms.NoteIDs = append(ms.NoteIDs, note.ObsID)
						}
					}
					ps.Months[month.Key] = ms
				}
			}
			out.Selected[proj.Key] = ps
		}
	}
	return out
}

// FlatNodes devuelve los nodos visibles en orden para navegación.
func FlatNodes(roots []*TreeNode) []*TreeNode {
	var out []*TreeNode
	for _, root := range roots {
		out = append(out, root)
		if root.Expanded {
			for _, month := range root.Children {
				out = append(out, month)
				if month.Expanded {
					out = append(out, month.Children...)
				}
			}
		}
	}
	return out
}

// Toggle alterna la selección de un nodo y propaga hacia hijos y padres.
func Toggle(node *TreeNode, roots []*TreeNode) {
	switch node.Check {
	case CheckFull:
		node.Check = CheckNone
		setAllChildren(node, CheckNone)
	default:
		node.Check = CheckFull
		setAllChildren(node, CheckFull)
	}
	// Recalcular padres
	updateParents(roots)
}

func updateParents(roots []*TreeNode) {
	for _, proj := range roots {
		for _, month := range proj.Children {
			month.Check = computeParentCheck(month.Children)
		}
		proj.Check = computeParentCheck(proj.Children)
	}
}

func countObs(months map[string][]store.Observation) int {
	n := 0
	for _, obs := range months {
		n += len(obs)
	}
	return n
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
