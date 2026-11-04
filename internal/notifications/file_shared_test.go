package notifications

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/luiscleto/shepherdr/internal/herdr"
)

func TestFileSharedStartsOffAndNamesThePlaceNotTheFiles(t *testing.T) {
	if DefaultEventSettings().FileShared {
		t.Fatal("File shared starts on")
	}
	var saved EventSettings
	if err := json.Unmarshal([]byte(`{"working":false,"blocked":true,"idle":false,"done":true,"unknown":false,"workspace_opened":false,"workspace_closed":false}`), &saved); err != nil || saved.FileShared {
		t.Fatalf("older settings = %+v, %v", saved, err)
	}
	snapshot := herdr.Snapshot{
		Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "spc", Label: "billing‮-api"}},
		Tabs:       []herdr.TabInfo{{TabID: "tab", WorkspaceID: "spc", Label: "review"}},
		Panes:      []herdr.PaneInfo{{PaneID: "t5", TerminalID: "core:t5", TabID: "tab", WorkspaceID: "spc"}},
	}
	source := "0123456789abcdef01234567"
	event := fileSharedEvent(herdr.BackendCore, snapshot, "t5", "core:t5", source)
	if !event.selected(EventSettings{FileShared: true}) || event.selected(DefaultEventSettings()) {
		t.Fatal("File shared selection")
	}
	payload, err := eventPayload(event)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"destination":"/#shared&herdr=core&source=` + source + `","herdr":"core","kind":"file_shared","name":"review","workspace_name":"billing-api"}`
	if got := strings.ReplaceAll(string(payload), `\u0026`, "&"); got != want {
		t.Fatalf("payload = %s\nwant      %s", payload, want)
	}
	outside, _ := eventPayload(fileSharedEvent("", herdr.Snapshot{}, "", "", ""))
	if string(outside) != `{"destination":"/#shared","kind":"file_shared"}` {
		t.Fatalf("outside payload = %s", outside)
	}
	if strings.Contains(string(payload), ".png") {
		t.Fatal("a file name reached the payload")
	}
}
