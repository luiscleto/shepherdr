package herdr

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// OtherTerminalsID names the last Home group: live terminals that no space
// places. App ids start with "sp", so it can't clash with a space.
const OtherTerminalsID = "other"

const otherTerminalsLabel = "Other terminals"

// ErrUnreadableAppLayout means the Herdr app's document is present but not
// the expected shape.
var ErrUnreadableAppLayout error = &CoreCompatibilityError{Detail: "The Herdr app's layout could not be read."}

type appSpace struct {
	ID      string
	Name    string
	Folder  string
	Machine bool
	Tabs    []appTab
}

type appTab struct {
	ID        string
	Name      string
	Terminals []string
}

func parseAppDocument(raw json.RawMessage) ([]appSpace, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var document struct {
		Spaces []json.RawMessage `json:"spaces"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Spaces == nil {
		return nil, ErrUnreadableAppLayout
	}
	spaces := make([]appSpace, 0, len(document.Spaces))
	for _, rawSpace := range document.Spaces {
		var space struct {
			ID      *string           `json:"id"`
			Name    any               `json:"name"`
			Folder  any               `json:"folder"`
			Machine any               `json:"machine"`
			Tabs    []json.RawMessage `json:"tabs"`
		}
		if err := json.Unmarshal(rawSpace, &space); err != nil || space.ID == nil || space.Tabs == nil {
			return nil, ErrUnreadableAppLayout
		}
		parsed := appSpace{ID: *space.ID, Name: stringOrEmpty(space.Name), Folder: stringOrEmpty(space.Folder), Machine: space.Machine != nil}
		for _, rawTab := range space.Tabs {
			var tab struct {
				ID   *string         `json:"id"`
				Name any             `json:"name"`
				Root json.RawMessage `json:"root"`
			}
			if err := json.Unmarshal(rawTab, &tab); err != nil || tab.ID == nil {
				return nil, ErrUnreadableAppLayout
			}
			parsed.Tabs = append(parsed.Tabs, appTab{ID: *tab.ID, Name: stringOrEmpty(tab.Name), Terminals: layoutTerminals(tab.Root, nil)})
		}
		spaces = append(spaces, parsed)
	}
	return spaces, nil
}

// layoutTerminals walks the split tree depth first, which is the app's visual
// order. Unknown node types and keys are ignored: a terminal they would have
// placed still appears under Other terminals.
func layoutTerminals(raw json.RawMessage, terminals []string) []string {
	var node struct {
		Type     string            `json:"type"`
		Children []json.RawMessage `json:"children"`
		Content  struct {
			Terminal any `json:"terminal"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &node) != nil {
		return terminals
	}
	switch node.Type {
	case "row", "column", "stack":
		for _, child := range node.Children {
			terminals = layoutTerminals(child, terminals)
		}
	case "block":
		if address, ok := node.Content.Terminal.(string); ok {
			terminals = append(terminals, address)
		}
	}
	return terminals
}

func stringOrEmpty(value any) string {
	text, _ := value.(string)
	return text
}

// ProjectCore turns one complete core read into the same Snapshot and Home
// contract the Herdr TUI produces. homeFolder names the app's "~".
func ProjectCore(read CoreRead, homeFolder string) (Snapshot, Home, error) {
	spaces, err := parseAppDocument(read.Document)
	if err != nil {
		return Snapshot{}, Home{}, err
	}

	live := make(map[string]CoreTerminal, len(read.Terminals))
	for _, terminal := range read.Terminals {
		// Only this machine's terminals: other machines' addresses carry a "/".
		if terminal.Address == "" || strings.Contains(terminal.Address, "/") {
			continue
		}
		if _, duplicate := live[terminal.Address]; duplicate {
			return Snapshot{}, Home{}, incoherent("terminal address is duplicated")
		}
		if terminal.Status != nil && !coreStatusValid(Status(terminal.Status.State)) {
			return Snapshot{}, Home{}, &InvalidStatusError{Status: terminal.Status.State}
		}
		live[terminal.Address] = terminal
	}

	placedIn := make(map[string]struct{})
	spaceIDs := make(map[string]struct{}, len(spaces))
	for _, space := range spaces {
		if _, duplicate := spaceIDs[space.ID]; duplicate || space.ID == OtherTerminalsID {
			return Snapshot{}, Home{}, incoherent("space id is duplicated")
		}
		spaceIDs[space.ID] = struct{}{}
		tabIDs := make(map[string]struct{}, len(space.Tabs))
		for _, tab := range space.Tabs {
			if _, duplicate := tabIDs[tab.ID]; duplicate {
				return Snapshot{}, Home{}, incoherent("tab id is duplicated")
			}
			tabIDs[tab.ID] = struct{}{}
			for _, address := range tab.Terminals {
				if _, duplicate := placedIn[address]; duplicate {
					return Snapshot{}, Home{}, incoherent("terminal is placed twice")
				}
				placedIn[address] = struct{}{}
			}
		}
	}

	// A worker the app doesn't place goes right after its top placed parent,
	// as the app nests sub-agents. Everything else goes to Other terminals.
	followers := make(map[string][]CoreTerminal)
	others := make([]CoreTerminal, 0)
	for address, terminal := range live {
		if _, placed := placedIn[address]; placed {
			continue
		}
		if parent := placedAncestor(terminal, live, placedIn); parent != "" {
			followers[parent] = append(followers[parent], terminal)
			continue
		}
		others = append(others, terminal)
	}
	for parent := range followers {
		sortByCreation(followers[parent])
	}
	sortByCreation(others)

	snapshot := Snapshot{Version: read.Version, Workspaces: []WorkspaceInfo{}, Tabs: []TabInfo{}, Panes: []PaneInfo{}, Agents: []AgentInfo{}}
	home := Home{Workspaces: []Workspace{}}
	addTerminal := func(workspace *Workspace, tab *Tab, terminal CoreTerminal) {
		projected := coreTerminal(read.CoreID, terminal)
		tab.Terminals = append(tab.Terminals, projected)
		snapshot.Panes = append(snapshot.Panes, PaneInfo{
			CWD: terminal.CWD, PaneID: projected.PaneID, TabID: tab.ID, TerminalID: projected.TerminalID,
			TerminalTitle: terminal.Title, TerminalTitleStripped: terminal.Title, WorkspaceID: workspace.ID,
		})
		if projected.Agent != nil {
			snapshot.Agents = append(snapshot.Agents, AgentInfo{
				AgentStatus: projected.Agent.Status, PaneID: projected.PaneID, TabID: tab.ID,
				TerminalID: projected.TerminalID, WorkspaceID: workspace.ID,
			})
		}
	}

	for spaceIndex, space := range spaces {
		workspace := Workspace{
			Actions: []WorkspaceAction{WorkspaceActionCloseWorkspace}, ID: space.ID, Label: spaceLabel(space, live, homeFolder),
			Number: uint(spaceIndex + 1), Tabs: []Tab{},
		}
		if !space.Machine {
			snapshot.Workspaces = append(snapshot.Workspaces, WorkspaceInfo{Label: workspace.Label, Number: workspace.Number, WorkspaceID: space.ID})
		}
		for tabIndex, appTab := range space.Tabs {
			tab := Tab{ID: appTab.ID, Label: appTab.Name, Number: uint(tabIndex + 1), Terminals: []Terminal{}}
			if tab.Label == "" {
				tab.Label = strconv.Itoa(tabIndex + 1)
			}
			for _, address := range appTab.Terminals {
				terminal, exists := live[address]
				if !exists {
					continue
				}
				addTerminal(&workspace, &tab, terminal)
				for _, follower := range followers[address] {
					addTerminal(&workspace, &tab, follower)
				}
			}
			if len(tab.Terminals) == 0 {
				continue
			}
			workspace.Tabs = append(workspace.Tabs, tab)
			snapshot.Tabs = append(snapshot.Tabs, TabInfo{Label: tab.Label, Number: tab.Number, PaneCount: uint(len(tab.Terminals)), TabID: tab.ID, WorkspaceID: space.ID})
		}
		if len(workspace.Tabs) == 0 {
			continue
		}
		home.Workspaces = append(home.Workspaces, workspace)
	}
	assignContextualTitles(&home)

	if len(others) > 0 {
		other := Workspace{
			Actions: []WorkspaceAction{}, ID: OtherTerminalsID, Label: otherTerminalsLabel,
			Number: uint(len(spaces) + 1), Tabs: []Tab{},
		}
		tab := Tab{ID: OtherTerminalsID, Label: otherTerminalsLabel, Number: 1, Terminals: []Terminal{}}
		for _, terminal := range others {
			addTerminal(&other, &tab, terminal)
		}
		other.Tabs = append(other.Tabs, tab)
		snapshot.Tabs = append(snapshot.Tabs, TabInfo{Label: tab.Label, Number: 1, PaneCount: uint(len(tab.Terminals)), TabID: tab.ID, WorkspaceID: other.ID})
		// The group's name never stands in for a lone terminal's title.
		if len(others) > 1 {
			group := Home{Workspaces: []Workspace{other}}
			assignContextualTitles(&group)
			other = group.Workspaces[0]
		}
		home.Workspaces = append(home.Workspaces, other)
	}

	for index := range home.Workspaces {
		counts := countAgents(home.Workspaces[index : index+1])
		if counts.nonzero() {
			home.Workspaces[index].AgentCounts = &counts
		}
		home.WorkingCount += counts.Working
		home.BlockedCount += counts.Blocked
		home.ErrorCount += counts.Error
	}
	return snapshot, home, nil
}

func coreStatusValid(status Status) bool {
	switch status {
	case StatusWorking, StatusBlocked, StatusIdle, StatusDone, StatusError:
		return true
	default:
		return false
	}
}

// Every terminal Home shows can be closed, including those under Other
// terminals; a space can be closed, but Other terminals can't.
func coreTerminal(coreID string, source CoreTerminal) Terminal {
	terminal := Terminal{Actions: []TerminalAction{TerminalActionClose}, PaneID: source.Address, TerminalID: coreID + ":" + source.Address}
	if source.Status != nil {
		name := source.Title
		if name == "" {
			name = source.Status.App
		}
		terminal.Agent = &Agent{Kind: source.Status.App, Name: name, Status: Status(source.Status.State)}
	}
	switch {
	case terminal.Agent != nil && terminal.Agent.Name != "":
		terminal.Title = terminal.Agent.Name
	case source.Name != nil && *source.Name != "":
		terminal.Title = *source.Name
	case source.Title != "":
		terminal.Title = source.Title
	default:
		terminal.Title = "Terminal"
	}
	return terminal
}

// placedAncestor follows live parents up to the first terminal the app
// places, so nested workers stay with the app terminal that started them.
func placedAncestor(terminal CoreTerminal, live map[string]CoreTerminal, placed map[string]struct{}) string {
	seen := map[string]struct{}{terminal.Address: {}}
	for terminal.Parent != nil {
		parent := *terminal.Parent
		if _, loop := seen[parent]; loop {
			return ""
		}
		seen[parent] = struct{}{}
		if _, ok := placed[parent]; ok {
			if _, alive := live[parent]; alive {
				return parent
			}
			return ""
		}
		next, alive := live[parent]
		if !alive {
			return ""
		}
		terminal = next
	}
	return ""
}

func sortByCreation(terminals []CoreTerminal) {
	sort.Slice(terminals, func(i, j int) bool {
		if terminals[i].CreatedMS != terminals[j].CreatedMS {
			return terminals[i].CreatedMS < terminals[j].CreatedMS
		}
		return terminals[i].Address < terminals[j].Address
	})
}

// spaceLabel matches the name the app shows: the space's own name; else
// what the app wrote into its first live terminal's "where" label (the git
// top folder); else the space folder's base name, "~" for home.
func spaceLabel(space appSpace, live map[string]CoreTerminal, homeFolder string) string {
	if space.Name != "" {
		return space.Name
	}
	for _, tab := range space.Tabs {
		for _, address := range tab.Terminals {
			terminal, exists := live[address]
			if !exists {
				continue
			}
			if where := terminal.Labels["where"]; where != "" {
				name, _, _ := strings.Cut(where, " / ")
				if name != "" {
					return name
				}
			}
			return folderLabel(space.Folder, homeFolder)
		}
	}
	return folderLabel(space.Folder, homeFolder)
}

func folderLabel(folder, homeFolder string) string {
	if folder == "" {
		return "Space"
	}
	cleaned := filepath.Clean(folder)
	if homeFolder != "" && cleaned == filepath.Clean(homeFolder) {
		return "~"
	}
	return filepath.Base(cleaned)
}

// CoreEndsWith lists the live terminals that end when address is killed: the
// terminal itself, then, down the chain, the workers it started that die with
// their parent.
func CoreEndsWith(terminals []CoreTerminal, address string) map[string]struct{} {
	ending := map[string]struct{}{}
	live := make(map[string]struct{}, len(terminals))
	for _, terminal := range terminals {
		live[terminal.Address] = struct{}{}
	}
	if _, ok := live[address]; !ok {
		return ending
	}
	ending[address] = struct{}{}
	for grown := true; grown; {
		grown = false
		for _, terminal := range terminals {
			if _, done := ending[terminal.Address]; done || !terminal.DieWithParent || terminal.Parent == nil {
				continue
			}
			if _, parentEnds := ending[*terminal.Parent]; parentEnds {
				ending[terminal.Address] = struct{}{}
				grown = true
			}
		}
	}
	return ending
}
