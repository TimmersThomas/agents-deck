// Package act performs the actions the display asks for.
package act

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

var surfaceRef = regexp.MustCompile(`^surface:[0-9]+$`)

type treeDoc struct {
	Windows []struct {
		Workspaces []workspaceDoc `json:"workspaces"`
	} `json:"windows"`
}

type workspaceDoc struct {
	ID     string     `json:"id"`
	Layout *layoutDoc `json:"layout"`
	Panes  []paneDoc  `json:"panes"`
}

type layoutDoc struct {
	Pane *struct {
		Ref string `json:"ref"`
	} `json:"pane"`
	Children []layoutDoc `json:"children"`
}

type paneDoc struct {
	Ref      string `json:"ref"`
	Selected string `json:"selected_surface_ref"`
	Surfaces []struct {
		Ref string `json:"ref"`
	} `json:"surfaces"`
}

// FirstPanel returns the surface to focus in a workspace: the selected surface of
// its first pane in layout order, which is the big left pane of the cw layout.
func FirstPanel(treeJSON []byte, workspaceID string) (string, error) {
	var doc treeDoc
	if err := json.Unmarshal(treeJSON, &doc); err != nil {
		return "", fmt.Errorf("parse cmux tree: %w", err)
	}
	for _, w := range doc.Windows {
		for _, ws := range w.Workspaces {
			if ws.ID == workspaceID {
				return ws.firstSurface()
			}
		}
	}
	return "", errors.New("workspace not in the cmux tree")
}

func (w workspaceDoc) firstSurface() (string, error) {
	if len(w.Panes) == 0 {
		return "", errors.New("workspace has no panes")
	}
	first := w.Panes[0]
	if w.Layout != nil {
		ref := w.Layout.firstPane()
		for _, p := range w.Panes {
			if p.Ref == ref {
				first = p
			}
		}
	}
	return first.surface()
}

func (l layoutDoc) firstPane() string {
	if l.Pane != nil {
		return l.Pane.Ref
	}
	for _, c := range l.Children {
		if ref := c.firstPane(); ref != "" {
			return ref
		}
	}
	return ""
}

func (p paneDoc) surface() (string, error) {
	ref := p.Selected
	if ref == "" && len(p.Surfaces) > 0 {
		ref = p.Surfaces[0].Ref
	}
	if !surfaceRef.MatchString(ref) {
		return "", errors.New("first pane has no usable surface")
	}
	return ref, nil
}
