package herdr

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"slices"
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
	// Waiting marks a space the document doesn't have yet: the app adds it
	// when its window next opens (rule 1b).
	Waiting bool
	Tabs    []appTab
}

type appTab struct {
	ID        string
	Name      string
	Terminals []string
	// Blocks maps each placed terminal to the app's id for its pane. It stays
	// on the server: only a split names it, to the app.
	Blocks map[string]string
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
			parsedTab := appTab{ID: *tab.ID, Name: stringOrEmpty(tab.Name), Blocks: map[string]string{}}
			layoutTerminals(tab.Root, &parsedTab)
			parsed.Tabs = append(parsed.Tabs, parsedTab)
		}
		spaces = append(spaces, parsed)
	}
	return spaces, nil
}

// layoutTerminals walks the split tree depth first, which is the app's visual
// order. Unknown node types and keys are ignored: a terminal they would have
// placed still appears under Other terminals.
func layoutTerminals(raw json.RawMessage, tab *appTab) {
	var node struct {
		Type     string            `json:"type"`
		ID       any               `json:"id"`
		Children []json.RawMessage `json:"children"`
		Content  struct {
			Terminal any `json:"terminal"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &node) != nil {
		return
	}
	switch node.Type {
	case "row", "column", "stack":
		for _, child := range node.Children {
			layoutTerminals(child, tab)
		}
	case "block":
		if address, ok := node.Content.Terminal.(string); ok {
			tab.Terminals = append(tab.Terminals, address)
			if id, ok := node.ID.(string); ok && id != "" {
				tab.Blocks[address] = id
			}
		}
	}
}

func stringOrEmpty(value any) string {
	text, _ := value.(string)
	return text
}

// ProjectCore turns one complete core read into the same Snapshot and Home
// contract the Herdr TUI produces. homeFolder names the app's "~". It sets no
// checkout_path: only Home's New space suggestions need one.
func ProjectCore(read CoreRead, homeFolder string) (Snapshot, Home, error) {
	return projectCore(read, homeFolder, nil)
}

// projectCore is ProjectCore with each shown space's repository folder from
// repositories, which only reads what is already known.
func projectCore(read CoreRead, homeFolder string, repositories func(folder string) string) (Snapshot, Home, error) {
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

	// While the window is open, the app writes a space it starts into the
	// document a moment after its terminal, with the space's name. So only a
	// terminal started before Shepherdr saw the window open waits in a space
	// of its own.
	var newSpacesBefore uint64
	if read.AppWindow == AppWindowOpen && !read.AppWindowOpened.IsZero() {
		newSpacesBefore = uint64(read.AppWindowOpened.UnixMilli())
	}
	spaces = placeWaiting(spaces, live, placedIn, newSpacesBefore)

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
	// New space, New terminal and New tab work with the app window open or
	// closed; Rename space and Rename tab only while it is open.
	paired := read.AppWindow != AppWindowUnpaired
	if paired {
		home.Actions = []HomeAction{HomeActionCreateSpace}
	}
	if read.AppWindow == AppWindowClosed {
		home.AppWindow = "closed"
	}
	addTerminal := func(workspace *Workspace, tab *Tab, terminal CoreTerminal, pane bool) {
		projected := coreTerminal(read.CoreID, terminal)
		// New terminal starts next to a terminal the app shows in a pane.
		if pane && paired {
			projected.Actions = slices.Insert(projected.Actions, len(projected.Actions)-1, TerminalActionSplit)
		}
		tab.Terminals = append(tab.Terminals, projected)
		snapshot.Panes = append(snapshot.Panes, PaneInfo{
			CWD: terminal.CWD, PaneID: projected.PaneID, TabID: tab.ID, TerminalID: projected.TerminalID,
			TerminalTitle: terminal.Title, TerminalTitleStripped: terminal.Title, WorkspaceID: workspace.ID,
		})
		// Notifications follow core's own word, so becoming seen never sends one.
		if terminal.Status != nil {
			snapshot.Agents = append(snapshot.Agents, AgentInfo{
				AgentStatus: Status(terminal.Status.State), PaneID: projected.PaneID, TabID: tab.ID,
				TerminalID: projected.TerminalID, WorkspaceID: workspace.ID,
			})
		}
	}

	for spaceIndex, space := range spaces {
		workspace := Workspace{
			Actions: []WorkspaceAction{WorkspaceActionCloseWorkspace}, ID: space.ID, Label: spaceLabel(space, live, homeFolder),
			Number: uint(spaceIndex + 1), Tabs: []Tab{},
		}
		renames := read.AppWindow == AppWindowOpen && !space.Waiting
		if renames {
			workspace.Actions = []WorkspaceAction{WorkspaceActionRenameSpace, WorkspaceActionCloseWorkspace}
		}
		// New tab needs a space the app already has, on this machine.
		if paired && !space.Waiting && !space.Machine {
			workspace.Actions = slices.Insert(workspace.Actions, 0, WorkspaceActionNewTab)
		}
		if space.Name != "" {
			name := space.Name
			workspace.ManualName = &name
		}
		if !space.Machine {
			snapshot.Workspaces = append(snapshot.Workspaces, WorkspaceInfo{Label: workspace.Label, Number: workspace.Number, WorkspaceID: space.ID})
		}
		for tabIndex, appTab := range space.Tabs {
			tab := Tab{ID: appTab.ID, Label: appTab.Name, Number: uint(tabIndex + 1), Terminals: []Terminal{}}
			if tab.Label == "" {
				tab.Label = strconv.Itoa(tabIndex + 1)
			} else {
				name := appTab.Name
				tab.ManualName = &name
			}
			for _, address := range appTab.Terminals {
				terminal, exists := live[address]
				if !exists {
					continue
				}
				_, pane := appTab.Blocks[address]
				addTerminal(&workspace, &tab, terminal, pane)
				for _, follower := range followers[address] {
					addTerminal(&workspace, &tab, follower, false)
				}
			}
			if len(tab.Terminals) == 0 {
				continue
			}
			workspace.Tabs = append(workspace.Tabs, tab)
			// The snapshot keeps only the name the person gave the tab, empty
			// when the app calls it by its number: notices may show that name.
			snapshot.Tabs = append(snapshot.Tabs, TabInfo{Label: appTab.Name, Number: tab.Number, PaneCount: uint(len(tab.Terminals)), TabID: tab.ID, WorkspaceID: space.ID})
		}
		if len(workspace.Tabs) == 0 {
			continue
		}
		// Home names a space's tabs only when it lists them, each in its row.
		if renames && len(workspace.Tabs) > 1 {
			workspace.Actions = slices.Insert(workspace.Actions, len(workspace.Actions)-1, WorkspaceActionRenameTab)
		}
		// New space suggests the repository folders of the spaces Home shows.
		// Another machine's folder isn't on this one.
		if repositories != nil && !space.Machine {
			workspace.CheckoutPath = repositories(space.Folder)
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
			addTerminal(&other, &tab, terminal, false)
		}
		other.Tabs = append(other.Tabs, tab)
		snapshot.Tabs = append(snapshot.Tabs, TabInfo{Number: 1, PaneCount: uint(len(tab.Terminals)), TabID: tab.ID, WorkspaceID: other.ID})
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

// placeWaiting applies rule 1b. A live terminal the document doesn't place,
// labelled with a herdr.space and a herdr.tab, is shown where the app puts it
// when its window next opens: after its tab's placed terminals; in a tab with
// that id at the end of its space; or in a space with that id after the
// document's, unnamed, in its first terminal's folder. Waiting terminals go in
// created_ms order and count as placed for their workers. A tab id that
// belongs to another space, the space id "other", or a new space for a
// terminal created at or after a nonzero newSpacesBefore leaves it under Other
// terminals. The app keeps these labels when it adopts a terminal, and the
// document always wins, so each read shows every terminal once.
func placeWaiting(spaces []appSpace, live map[string]CoreTerminal, placedIn map[string]struct{}, newSpacesBefore uint64) []appSpace {
	waiting := make([]CoreTerminal, 0)
	for address, terminal := range live {
		if _, placed := placedIn[address]; placed {
			continue
		}
		spaceID, tabID := terminal.Labels["herdr.space"], terminal.Labels["herdr.tab"]
		if spaceID != "" && tabID != "" && spaceID != OtherTerminalsID {
			waiting = append(waiting, terminal)
		}
	}
	sortByCreation(waiting)
	spaceOf := make(map[string]int, len(spaces))
	tabSpace := make(map[string]string)
	for index, space := range spaces {
		spaceOf[space.ID] = index
		for _, tab := range space.Tabs {
			tabSpace[tab.ID] = space.ID
		}
	}
	for _, terminal := range waiting {
		spaceID, tabID := terminal.Labels["herdr.space"], terminal.Labels["herdr.tab"]
		if owner, known := tabSpace[tabID]; known && owner != spaceID {
			continue
		}
		index, known := spaceOf[spaceID]
		if !known && newSpacesBefore != 0 && terminal.CreatedMS >= newSpacesBefore {
			continue
		}
		if !known {
			spaces = append(spaces, appSpace{ID: spaceID, Folder: terminal.CWD, Waiting: true})
			index = len(spaces) - 1
			spaceOf[spaceID] = index
		}
		space := &spaces[index]
		tabIndex := slices.IndexFunc(space.Tabs, func(tab appTab) bool { return tab.ID == tabID })
		if tabIndex < 0 {
			space.Tabs = append(space.Tabs, appTab{ID: tabID, Blocks: map[string]string{}})
			tabIndex = len(space.Tabs) - 1
			tabSpace[tabID] = spaceID
		}
		space.Tabs[tabIndex].Terminals = append(space.Tabs[tabIndex].Terminals, terminal.Address)
		placedIn[terminal.Address] = struct{}{}
	}
	return spaces
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
// terminals; a space can be closed, but Other terminals can't. An agent can
// be pinned or unpinned.
func coreTerminal(coreID string, source CoreTerminal) Terminal {
	terminal := Terminal{Actions: []TerminalAction{TerminalActionClose}, PaneID: source.Address, TerminalID: coreID + ":" + source.Address}
	// The agent name is the app core reports, never the live title, so a row
	// keeps its name while the agent works.
	if source.Status != nil {
		pinned := corePinned(source)
		terminal.Agent = &Agent{Kind: source.Status.App, Name: source.Status.App, Status: CoreShownStatus(source), Pinned: pinned}
		pin := TerminalActionPin
		if pinned {
			pin = TerminalActionUnpin
		}
		terminal.Actions = []TerminalAction{pin, TerminalActionClose}
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

// CorePaneBlock finds the app's pane id for a terminal the read's document
// places in that space and tab.
func CorePaneBlock(read CoreRead, spaceID, tabID, address string) (string, bool) {
	spaces, err := parseAppDocument(read.Document)
	if err != nil {
		return "", false
	}
	for _, space := range spaces {
		for _, tab := range space.Tabs {
			if space.ID == spaceID && tab.ID == tabID {
				block, ok := tab.Blocks[address]
				return block, ok
			}
		}
	}
	return "", false
}

// CoreSpace finds a space the read's document has, with its folder and how
// many tabs the app holds for it.
func CoreSpace(read CoreRead, spaceID string) (folder string, tabs int, found bool) {
	spaces, err := parseAppDocument(read.Document)
	if err != nil {
		return "", 0, false
	}
	for _, space := range spaces {
		if space.ID == spaceID {
			return space.Folder, len(space.Tabs), true
		}
	}
	return "", 0, false
}
