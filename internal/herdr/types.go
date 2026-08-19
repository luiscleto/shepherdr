package herdr

import (
	"fmt"
	"sort"
)

const Protocol = 19

type Status string

const (
	StatusWorking Status = "working"
	StatusBlocked Status = "blocked"
	StatusIdle    Status = "idle"
	StatusDone    Status = "done"
	StatusUnknown Status = "unknown"
)

func (s Status) Valid() bool {
	switch s {
	case StatusWorking, StatusBlocked, StatusIdle, StatusDone, StatusUnknown:
		return true
	default:
		return false
	}
}

type AgentSession struct {
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Value  string `json:"value"`
}

type AgentInfo struct {
	Agent          *string       `json:"agent"`
	AgentSession   *AgentSession `json:"agent_session"`
	AgentStatus    Status        `json:"agent_status"`
	DisplayAgent   *string       `json:"display_agent"`
	Name           *string       `json:"name"`
	PaneID         string        `json:"pane_id"`
	Revision       uint64        `json:"revision"`
	StateChangeSeq uint64        `json:"state_change_seq"`
	TabID          string        `json:"tab_id"`
	TerminalID     string        `json:"terminal_id"`
	Title          *string       `json:"title"`
	WorkspaceID    string        `json:"workspace_id"`
}

type PaneInfo struct {
	Focused               bool   `json:"focused"`
	PaneID                string `json:"pane_id"`
	TabID                 string `json:"tab_id"`
	TerminalID            string `json:"terminal_id"`
	TerminalTitle         string `json:"terminal_title"`
	TerminalTitleStripped string `json:"terminal_title_stripped"`
	WorkspaceID           string `json:"workspace_id"`
}

type TabInfo struct {
	Focused     bool   `json:"focused"`
	Label       string `json:"label"`
	Number      uint   `json:"number"`
	PaneCount   uint   `json:"pane_count"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

type WorkspaceInfo struct {
	ActiveTabID string `json:"active_tab_id"`
	Focused     bool   `json:"focused"`
	Label       string `json:"label"`
	Number      uint   `json:"number"`
	WorkspaceID string `json:"workspace_id"`
}

type Rectangle struct {
	Height int `json:"height"`
	Width  int `json:"width"`
	X      int `json:"x"`
	Y      int `json:"y"`
}

type LayoutPane struct {
	Focused bool      `json:"focused"`
	PaneID  string    `json:"pane_id"`
	Rect    Rectangle `json:"rect"`
}

type LayoutInfo struct {
	FocusedPaneID string       `json:"focused_pane_id"`
	Panes         []LayoutPane `json:"panes"`
	TabID         string       `json:"tab_id"`
	WorkspaceID   string       `json:"workspace_id"`
	Zoomed        bool         `json:"zoomed"`
}

type Snapshot struct {
	Agents             []AgentInfo     `json:"agents"`
	FocusedPaneID      string          `json:"focused_pane_id"`
	FocusedTabID       string          `json:"focused_tab_id"`
	FocusedWorkspaceID string          `json:"focused_workspace_id"`
	Layouts            []LayoutInfo    `json:"layouts"`
	Panes              []PaneInfo      `json:"panes"`
	Protocol           uint32          `json:"protocol"`
	Tabs               []TabInfo       `json:"tabs"`
	Version            string          `json:"version"`
	Workspaces         []WorkspaceInfo `json:"workspaces"`
}

type Agent struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Status Status `json:"status"`
}

type Terminal struct {
	Agent      *Agent `json:"agent,omitempty"`
	PaneID     string `json:"pane_id"`
	TerminalID string `json:"terminal_id"`
	Title      string `json:"title"`
}

type Tab struct {
	Current   bool       `json:"current"`
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	Number    uint       `json:"number"`
	Terminals []Terminal `json:"terminals"`
}

type Workspace struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Number uint   `json:"number"`
	Tabs   []Tab  `json:"tabs"`
}

type Home struct {
	BlockedCount int         `json:"blocked_count"`
	WorkingCount int         `json:"working_count"`
	Workspaces   []Workspace `json:"workspaces"`
}

func Project(snapshot Snapshot) (Home, error) {
	workspaceSources := make(map[string]WorkspaceInfo, len(snapshot.Workspaces))
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID == "" {
			return Home{}, incoherent("workspace has an empty id")
		}
		if _, exists := workspaceSources[workspace.WorkspaceID]; exists {
			return Home{}, incoherent("workspace id is duplicated")
		}
		workspaceSources[workspace.WorkspaceID] = workspace
	}

	tabSources := make(map[string]TabInfo, len(snapshot.Tabs))
	tabsByWorkspace := make(map[string][]TabInfo, len(snapshot.Workspaces))
	for _, tab := range snapshot.Tabs {
		if tab.TabID == "" {
			return Home{}, incoherent("tab has an empty id")
		}
		if _, exists := tabSources[tab.TabID]; exists {
			return Home{}, incoherent("tab id is duplicated")
		}
		if _, exists := workspaceSources[tab.WorkspaceID]; !exists {
			return Home{}, incoherent("tab references an unknown workspace")
		}
		tabSources[tab.TabID] = tab
		tabsByWorkspace[tab.WorkspaceID] = append(tabsByWorkspace[tab.WorkspaceID], tab)
	}
	for _, workspace := range snapshot.Workspaces {
		if workspace.ActiveTabID == "" {
			continue
		}
		tab, exists := tabSources[workspace.ActiveTabID]
		if !exists || tab.WorkspaceID != workspace.WorkspaceID {
			return Home{}, incoherent("workspace active tab is unknown or mismatched")
		}
	}

	paneSources := make(map[string]PaneInfo, len(snapshot.Panes))
	terminalIDs := make(map[string]struct{}, len(snapshot.Panes))
	panesByTab := make(map[string][]PaneInfo, len(snapshot.Tabs))
	for _, pane := range snapshot.Panes {
		if pane.PaneID == "" || pane.TerminalID == "" {
			return Home{}, incoherent("pane has an empty pane or terminal id")
		}
		if _, exists := paneSources[pane.PaneID]; exists {
			return Home{}, incoherent("pane id is duplicated")
		}
		if _, exists := terminalIDs[pane.TerminalID]; exists {
			return Home{}, incoherent("terminal id is duplicated")
		}
		tab, exists := tabSources[pane.TabID]
		if !exists || tab.WorkspaceID != pane.WorkspaceID {
			return Home{}, incoherent("pane references an unknown or mismatched tab")
		}
		if _, exists := workspaceSources[pane.WorkspaceID]; !exists {
			return Home{}, incoherent("pane references an unknown workspace")
		}
		paneSources[pane.PaneID] = pane
		terminalIDs[pane.TerminalID] = struct{}{}
		panesByTab[pane.TabID] = append(panesByTab[pane.TabID], pane)
	}
	if snapshot.FocusedWorkspaceID != "" {
		if _, exists := workspaceSources[snapshot.FocusedWorkspaceID]; !exists {
			return Home{}, incoherent("focused workspace is unknown")
		}
	}
	if snapshot.FocusedTabID != "" {
		tab, exists := tabSources[snapshot.FocusedTabID]
		if !exists || (snapshot.FocusedWorkspaceID != "" && tab.WorkspaceID != snapshot.FocusedWorkspaceID) {
			return Home{}, incoherent("focused tab is unknown or mismatched")
		}
	}
	if snapshot.FocusedPaneID != "" {
		pane, exists := paneSources[snapshot.FocusedPaneID]
		if !exists || (snapshot.FocusedWorkspaceID != "" && pane.WorkspaceID != snapshot.FocusedWorkspaceID) ||
			(snapshot.FocusedTabID != "" && pane.TabID != snapshot.FocusedTabID) {
			return Home{}, incoherent("focused pane is unknown or mismatched")
		}
	}

	agentsByPane := make(map[string]Agent, len(snapshot.Agents))
	home := Home{Workspaces: []Workspace{}}
	for _, source := range snapshot.Agents {
		if !source.AgentStatus.Valid() {
			return Home{}, &InvalidStatusError{Status: string(source.AgentStatus)}
		}
		pane, exists := paneSources[source.PaneID]
		if !exists || pane.WorkspaceID != source.WorkspaceID || pane.TabID != source.TabID || pane.TerminalID != source.TerminalID {
			return Home{}, incoherent("agent references an unknown or mismatched terminal")
		}
		if _, exists := agentsByPane[source.PaneID]; exists {
			return Home{}, incoherent("pane has duplicate agent decoration")
		}
		name := firstNonEmpty(source.Name, source.Title, source.DisplayAgent, source.Agent)
		kind := valueOrEmpty(source.DisplayAgent)
		if kind == "" {
			kind = valueOrEmpty(source.Agent)
		}
		if name == "" {
			name = "Agent"
		}
		agentsByPane[source.PaneID] = Agent{Kind: kind, Name: name, Status: source.AgentStatus}
		switch source.AgentStatus {
		case StatusWorking:
			home.WorkingCount++
		case StatusBlocked:
			home.BlockedCount++
		}
	}

	layoutsByTab := make(map[string][]LayoutInfo, len(snapshot.Layouts))
	for _, layout := range snapshot.Layouts {
		layoutsByTab[layout.TabID] = append(layoutsByTab[layout.TabID], layout)
	}

	orderedWorkspaces := append([]WorkspaceInfo(nil), snapshot.Workspaces...)
	sort.Slice(orderedWorkspaces, func(i, j int) bool {
		if orderedWorkspaces[i].Number != orderedWorkspaces[j].Number {
			return orderedWorkspaces[i].Number < orderedWorkspaces[j].Number
		}
		return orderedWorkspaces[i].WorkspaceID < orderedWorkspaces[j].WorkspaceID
	})
	for _, workspaceSource := range orderedWorkspaces {
		workspace := Workspace{
			ID: workspaceSource.WorkspaceID, Label: displayWorkspaceLabel(workspaceSource.Label), Number: workspaceSource.Number, Tabs: []Tab{},
		}
		tabs := append([]TabInfo(nil), tabsByWorkspace[workspace.ID]...)
		sort.Slice(tabs, func(i, j int) bool {
			if tabs[i].Number != tabs[j].Number {
				return tabs[i].Number < tabs[j].Number
			}
			return tabs[i].TabID < tabs[j].TabID
		})
		for _, tabSource := range tabs {
			tab := Tab{
				Current: tabSource.TabID == workspaceSource.ActiveTabID,
				ID:      tabSource.TabID, Label: displayTabLabel(tabSource.Label), Number: tabSource.Number, Terminals: []Terminal{},
			}
			for _, pane := range orderPanes(panesByTab[tab.ID], layoutsByTab[tab.ID], workspace.ID, tab.ID) {
				terminal := Terminal{
					PaneID: pane.PaneID, TerminalID: pane.TerminalID, Title: displayTerminalTitle(pane),
				}
				if agent, exists := agentsByPane[pane.PaneID]; exists {
					copy := agent
					terminal.Agent = &copy
					if agent.Name != "Agent" {
						terminal.Title = agent.Name
					}
				}
				tab.Terminals = append(tab.Terminals, terminal)
			}
			workspace.Tabs = append(workspace.Tabs, tab)
		}
		home.Workspaces = append(home.Workspaces, workspace)
	}

	assignContextualTitles(&home)
	return home, nil
}

func orderPanes(panes []PaneInfo, layouts []LayoutInfo, workspaceID, tabID string) []PaneInfo {
	fallback := append([]PaneInfo(nil), panes...)
	sort.Slice(fallback, func(i, j int) bool { return fallback[i].PaneID < fallback[j].PaneID })
	if len(layouts) != 1 || layouts[0].WorkspaceID != workspaceID || layouts[0].TabID != tabID || layouts[0].Zoomed {
		return fallback
	}
	layout := layouts[0]
	if len(layout.Panes) != len(panes) {
		return fallback
	}
	byID := make(map[string]PaneInfo, len(panes))
	for _, pane := range panes {
		byID[pane.PaneID] = pane
	}
	type positionedPane struct {
		pane PaneInfo
		rect Rectangle
	}
	positioned := make([]positionedPane, 0, len(layout.Panes))
	seen := make(map[string]struct{}, len(layout.Panes))
	for _, candidate := range layout.Panes {
		pane, exists := byID[candidate.PaneID]
		if !exists || candidate.Rect.Width <= 0 || candidate.Rect.Height <= 0 {
			return fallback
		}
		if _, duplicate := seen[candidate.PaneID]; duplicate {
			return fallback
		}
		seen[candidate.PaneID] = struct{}{}
		positioned = append(positioned, positionedPane{pane: pane, rect: candidate.Rect})
	}
	sort.Slice(positioned, func(i, j int) bool {
		if positioned[i].rect.Y != positioned[j].rect.Y {
			return positioned[i].rect.Y < positioned[j].rect.Y
		}
		if positioned[i].rect.X != positioned[j].rect.X {
			return positioned[i].rect.X < positioned[j].rect.X
		}
		return positioned[i].pane.PaneID < positioned[j].pane.PaneID
	})
	ordered := make([]PaneInfo, 0, len(positioned))
	for _, item := range positioned {
		ordered = append(ordered, item.pane)
	}
	return ordered
}

func assignContextualTitles(home *Home) {
	flattenedCounts := make(map[string]int)
	for workspaceIndex := range home.Workspaces {
		workspace := &home.Workspaces[workspaceIndex]
		if terminalCount(*workspace) != 1 {
			continue
		}
		flattenedCounts[workspace.Label]++
		for tabIndex := range workspace.Tabs {
			if len(workspace.Tabs[tabIndex].Terminals) == 1 {
				workspace.Tabs[tabIndex].Terminals[0].Title = workspace.Label
			}
		}
	}
	flattenedSeen := make(map[string]int)
	for workspaceIndex := range home.Workspaces {
		workspace := &home.Workspaces[workspaceIndex]
		if terminalCount(*workspace) == 1 && flattenedCounts[workspace.Label] > 1 {
			flattenedSeen[workspace.Label]++
			for tabIndex := range workspace.Tabs {
				if len(workspace.Tabs[tabIndex].Terminals) == 1 {
					workspace.Tabs[tabIndex].Terminals[0].Title = fmt.Sprintf("%s %d", workspace.Label, flattenedSeen[workspace.Label])
				}
			}
		}
	}

	for workspaceIndex := range home.Workspaces {
		workspace := &home.Workspaces[workspaceIndex]
		if terminalCount(*workspace) <= 1 {
			continue
		}
		showTabs := nonEmptyTabCount(*workspace) > 1
		counts := make(map[string]int)
		for tabIndex := range workspace.Tabs {
			for terminalIndex := range workspace.Tabs[tabIndex].Terminals {
				terminal := &workspace.Tabs[tabIndex].Terminals[terminalIndex]
				counts[titleScope(tabIndex, terminal.Title, showTabs)]++
			}
		}
		seen := make(map[string]int)
		for tabIndex := range workspace.Tabs {
			for terminalIndex := range workspace.Tabs[tabIndex].Terminals {
				terminal := &workspace.Tabs[tabIndex].Terminals[terminalIndex]
				key := titleScope(tabIndex, terminal.Title, showTabs)
				if counts[key] > 1 {
					seen[key]++
					terminal.Title = fmt.Sprintf("%s %d", terminal.Title, seen[key])
				}
			}
		}
	}
}

func titleScope(tabIndex int, title string, showTabs bool) string {
	if showTabs {
		return fmt.Sprintf("%d\x00%s", tabIndex, title)
	}
	return title
}

func terminalCount(workspace Workspace) int {
	total := 0
	for _, tab := range workspace.Tabs {
		total += len(tab.Terminals)
	}
	return total
}

func nonEmptyTabCount(workspace Workspace) int {
	total := 0
	for _, tab := range workspace.Tabs {
		if len(tab.Terminals) > 0 {
			total++
		}
	}
	return total
}

func displayWorkspaceLabel(label string) string {
	if label == "" {
		return "Workspace"
	}
	return label
}

func displayTabLabel(label string) string {
	if label == "" {
		return "Tab"
	}
	return label
}

func displayTerminalTitle(pane PaneInfo) string {
	if pane.TerminalTitleStripped != "" {
		return pane.TerminalTitleStripped
	}
	if pane.TerminalTitle != "" {
		return pane.TerminalTitle
	}
	return "Terminal"
}

func incoherent(detail string) error {
	return &IncoherentSnapshotError{Detail: detail}
}

func firstNonEmpty(values ...*string) string {
	for _, value := range values {
		if value != nil && *value != "" {
			return *value
		}
	}
	return ""
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
