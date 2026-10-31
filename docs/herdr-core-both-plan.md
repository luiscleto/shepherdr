# Herdr TUI and Herdr Core together

Status: **approved human direction and work brief for wave 1b** (2026-10-31). The human approved this plan with decisions D1–D6 in section 12 as recorded there. It amends `docs/herdr-core-plan.md`; wave 1b applies the doc edits in section 9.

Planning base: `e0e7446` (master, wave 1 "Home on core" integrated).

This plan applies the human's new direction (one Shepherdr follows both, Home switches between **Herdr TUI** and **Herdr Core** per browser, notifications from both). It replaces decision 1 and the section 2 flag rules of `docs/herdr-core-plan.md`. It does not reopen anything else in that plan. The small interface choices the new direction left open are recorded in section 12.

Terms used below: **side** means one Herdr: `tui` (the Herdr TUI) or `core` (herdr-core, behind the Herdr app). **Present** is defined in 3.1.

---

## 0. Summary

- The server runs both projectors all the time, each with its own socket, retry loop, connection state and Home. Neither depends on the other. A side that is not installed just stays **Herdr is not running**.
- The Home WebSocket sends one complete message carrying both sides' complete states, `{epoch, tui, core}`. It is sent whenever either side changes. There is no client-to-server selection, replay or reconciliation. Each side keeps one complete Herdr read and one complete browser update.
- The browser picks the side to show. It remembers the choice in `localStorage` and falls back to memory when storage is unavailable. A switch appears only when both sides are present.
- Ids are left unchanged. Every reference also names its side: Home rows (through the side being shown), `#terminal=…&herdr=core`, notification links and payloads, one upload-records file per side, and exact-target checks against that side's state only. No id is shared between sides, so a collision has nowhere to happen.
- Notifications use one subscription and one settings set, with one evaluator per side. Each payload names its side.
- Uploads: the existing manager and `uploads.json` serve the TUI only and observe only the TUI projector. Core gets its own manager and `uploads-core.json` in wave 2, when core file sends arrive.
- New wave **1b** ("both at once", one worker) runs before wave 2. It also fixes rereview finding A.

---

## 1. Server

### 1.1 Start and flags

- Delete `-herdr-core`. Keep `-herdr-socket` (default unchanged) and `-herdr-core-socket` (default `$XDG_RUNTIME_DIR/herdr-core/default.sock`).
- Start errors that remain:
  - a relative `-herdr-socket` or `-herdr-core-socket`;
  - access commands combined with either socket flag.
- Start errors that go away:
  - "-herdr-core-socket requires -herdr-core";
  - "-herdr-core cannot be combined with -herdr-socket";
  - "-herdr-core-socket must be set when XDG_RUNTIME_DIR is not". Instead, when `XDG_RUNTIME_DIR` is unset and the flag isn't given, core is **not followed**. Shepherdr logs one info line at start ("Herdr Core is not followed: set -herdr-core-socket"), and the core side is a fixed `not_running` state with no Home, so it is never present. This keeps TUI-only macOS starts working. Today they would fail.
- `herdr` on `PATH`: core-only machines don't have it, so its absence can't stop the start. Look it up once at start. If it is missing, log one warning ("Herdr TUI terminals can't open: herdr is not on PATH") and create no TUI terminal bridge. The TUI side still gets Home, actions and notifications, and opening a TUI terminal shows the existing **Terminal unavailable**. This follows from the new decision 1 and needs no new copy.
- The ready log has one line with `herdr_socket=…` and `herdr_core_socket=…` (empty when core isn't followed). This replaces the two mode-specific lines.

### 1.2 Two projectors side by side

- `main.go` always builds `tuiProjector := herdr.NewProjector(herdr.NewClient(tuiSocket))` and, unless core isn't followed, `coreProjector := herdr.NewCoreProjector(...)`, and runs both with `go p.Run(ctx)`. The projector code (both loops, the backend seam, retry each second, the one-read latch, `markGap`/`publishError`) is unchanged. Each projector already keeps its own `State`, `Gap` and listeners, so the two connection states are independent by construction.
- Wiring per side:

| | Herdr TUI | Herdr Core |
|---|---|---|
| Snapshot observer (notifications) | `notificationManager.Observer(BackendTUI)` | `notificationManager.Observer(BackendCore)` |
| Published-snapshot observer (uploads) | the existing upload manager (`uploads.json`) | none in 1b; its own manager in wave 2 (section 5) |
| Workspace and terminal actions | existing action client; refresh = TUI projector | none (core has no actions, plan 3.5; `docs/herdr-core-actions-plan.md` adds core actions) |
| Terminal bridge, exact target, Reader read | existing bridge, targets = TUI projector | none in 1b; core bridge in wave 2 |
| File-send state source | TUI projector | wave 2 |

- Lifecycle for each side, independently:
  - **Never installed:** the socket is missing, so the side is `not_running` forever with no Home. It is never present, and its dial each second is cheap. `not_running` is logged only when it changes, as today.
  - **Starts later:** the next retry dials, reads and publishes a Home. The side becomes present and the switch appears (3.2).
  - **Stops:** the existing behaviour applies. The side shows **Reconnecting** or **Herdr is not running**, keeps its last-known Home, and stays present. The other side is untouched: it doesn't blip and isn't re-read.
  - **Restarts:** Reconnecting, then Live after one fresh complete read, with a notification baseline (6.2).
  - **Incompatible:** **Cannot use this Herdr** with its detail. The side is present (3.1).

### 1.3 `server.New` and the Home socket

- `server.New` takes both projectors (core may be a fixed not-followed source) and gives the TUI one to everything TUI-only.
- `homeSocket` subscribes to both. On a signal from either, it writes **one** message with both sides' `Current()` state. Writing both complete states each time is the whole contract: a browser that misses frames needs nothing replayed, and the next frame is complete. Heartbeats are unchanged.

---

## 2. State JSON and browser contract

```json
{"epoch": "…",
 "tui":  {"can_open_terminals": true, "connection": "live", "gap": 3, "has_home": true, "herdr_version": "0.9.5",
          "home": { …existing Home… }, "last_known": false},
 "core": {"can_open_terminals": false, "connection": "not_running", "gap": 0, "has_home": false, "herdr_version": "",
          "home": {"workspaces": [], "working_count": 0, "blocked_count": 0, "error_count": 0},
          "last_known": false}}
```

- Each side object is today's `State` without the wave 1 `herdr` field, which goes away because the key names the side. `detail` stays optional.
- `can_open_terminals` (required) says whether that side's terminals can open. The Herdr TUI's is true when the TUI terminal bridge exists, and false when `herdr` isn't on `PATH`. Herdr Core's is false until wave 2 (10.2). When it is false, the Terminal page shows the existing **Terminal unavailable** at once, without "This terminal is no longer here.". This is the one rule for both sides; the Terminal page reads no HTTP status. (Added after the wave 1b review, open question 1.)
- The parser requires exactly `epoch`, `tui` and `core`, and parses each side with today's exact-key rules.
- **Why both Homes and not just the selected one:** the browser needs the other side to decide presence and the switch, to open a notification for the other side (the Terminal route checks that side's Home), and to switch with no Loading flash. A server-side selection would need a client-to-server message and per-connection state, which is the machinery AGENTS.md says to avoid. The cost is about double the Home bytes per update, which is small next to terminal traffic.
- `gap` and `last_known` are per side and mean what they mean today.

---

## 3. Presence, badges and the switch

### 3.1 Rules (browser, from existing fields only)

- **Settled:** a side is settled unless it is `reconnecting` with no Home. That combination happens only during the first attempt after Shepherdr starts. A missing socket fails at once, and a hung peer is bounded by the existing 5 s request timeout.
- **Present:** `has_home` (Shepherdr has had a Home from this side since it started), or `connection == "incompatible"` (it is running but can't be used, which the person should see).
  - A side that stops keeps `has_home`, so it stays present until Shepherdr restarts. The switch therefore never flickers on a Herdr restart or a network blip, and the page doesn't jump away from a Home the person is looking at. Hiding a stopped side is the alternative; see decision D2.
  - "Never installed" or "not running since Shepherdr started" is not present.
- **Switch shown:** when both sides are present.

### 3.2 Which side Home shows

1. Until both sides are settled, Home shows the existing **Loading terminals**. This avoids a one-side-then-switch pop-in when the page loads.
2. Initial choice: the remembered side if it is present. Otherwise the only present side. If both are present and nothing is remembered: **Herdr Core** (decision D1). If neither is present: Herdr TUI, which shows its existing **Herdr is not running**.
3. Once a side is shown, only these change it:
   - a tap on the switch, which is remembered;
   - opening a terminal from the other side, which lasts for this visit only (decision D4);
   - the shown side no longer being present, which can only follow a Shepherdr restart (new epoch). Then step 2 runs again.

   When a side appears later, the switch appears with the current side still selected. The page doesn't move.
4. Storage: `localStorage["shepherdr.home.herdr"] = "tui" | "core"`, read once at load and written only on a switch tap. Every access is wrapped in `try`/`catch`, as `notifications.ts` already does for its not-now flag. Without storage the choice lasts for the visit, and a reload uses step 2. Any other stored value is ignored.

### 3.3 Badges per side

- The badge uses the existing words, computed from the **shown** side's state exactly as today: **Live**, **Reconnecting**, **Herdr is not running** and **Cannot use this Herdr** (with that side's detail).
- **Offline** is browser-wide. It is derived from Home frames, which carry both sides, so it applies to whichever side is shown.
- The switch shows only the two labels. It shows no badge, count or dot for the other side: no combined Home, as asked. Tapping a side that isn't running shows its last-known Home under **Herdr is not running**, or **Cannot use this Herdr**, exactly as a single-side Home does today.
- Switching changes nothing on the server and is never a Loading or Reconnecting state, because both states are already in the browser.

---

## 4. Identity

**Format:** a terminal reference is `(side, pane_id, terminal_id)` and a group reference is `(side, workspace_id)`, with `side ∈ {tui, core}`. The ids keep their current forms:

| | Herdr TUI | Herdr Core |
|---|---|---|
| workspace / space | `w1` (opaque) | `sp…`, or `other` |
| tab | `w1:t1` | `tb…` |
| `pane_id` | `w1:p1` | `t7` |
| `terminal_id` | opaque | `<core.id>:t7` |

The formats differ in practice, but TUI ids are opaque (discovery: "IDs must be read from responses"), so the plan does not rely on that. Instead, no lookup ever crosses sides:

- **Home rows:** a row belongs to the shown side's Home. The Home view keys its row and menu caches by `pane_id`, so it **clears its per-row caches when the shown side changes** (an explicit, rare action, so rebuilding the list is fine). `selectedTerminal`, `homeFocusPane` and the restore place carry the side.
- **`#terminal` route:** `#terminal=<pane>&terminal_id=<id>&herdr=<tui|core>`. Every new link includes `herdr`: rows, created-terminal links and notifications. A missing `herdr` means **tui**. That covers TUI notifications and bookmarks from released versions; core was never released, so no core link without `herdr` exists outside test runs. Any other value means **Terminal unavailable**. The route is resolved only against that side's Home.
- **Notification links and payloads:** the destination carries `&herdr=<side>`, and the payload always carries `"herdr": "tui" | "core"` (section 6).
- **Uploads:** one records file per side (section 5), so TUI `w1` and a core space can never share a record, whatever their ids.
- **Exact-target checks:** each bridge checks targets only against its own projector: TUI against the TUI projector (unchanged code), core against the core projector (wave 2). In wave 2 the terminal endpoints (`/api/terminal`, `/api/terminal/read`, `/api/terminal/files`) take a **required** `herdr` parameter that picks the bridge. The browser and server ship together, so a page left open from an older build gets the existing failure state rather than a guess.
- **Workspace and terminal actions:** TUI only. Core Homes have no actions, so nothing can be sent for core. (Note: `docs/herdr-core-actions-plan.md` adds core actions.)

---

## 5. Uploads

**Rule:** each side has its own upload manager and records file, and each manager observes only its own projector. Both share `-upload-parent`. Directory names are `shepherdr-<label>-<random 128-bit>`, and cleanup only ever touches directories in its own records with a matching marker, so the two managers can't remove each other's folders.

- **Wave 1b:** the existing manager (`uploads.json`) is wired with `tuiProjector.SetPublishedSnapshotObserver(uploadManager)`, unconditionally. The core projector has no uploads observer, because core can't send files yet. This is the wave 1 fix done structurally rather than by a mode check. Neither core reads nor core restarts can reach TUI records, and existing `uploads.json` files need no migration.
- **Wave 2:** a second `uploads.Open(uploadParent, <config>/shepherdr/uploads-core.json)` is wired only to the core projector and used only by `herdr=core` file sends.
  - **Other terminals** (the wave 1 open question): today a record for `other` would never be seen as present, because the core `Snapshot.Workspaces` lists document spaces only. It would then be deleted at the next Shepherdr start while the terminal still lives. **Approved (decision D5):** treat **Other terminals** as one group for files. Its files stay while Home lists the group and are cleaned up once it is gone, as for a space. Implementation choice for the wave 2 worker: the core uploads observer counts `other` as present while any pane has `WorkspaceID == other`. It must not add `other` to `Snapshot.Workspaces`, because space notices rely on its absence.
  - Startup reconciliation stays correct per side. If core isn't running when Shepherdr starts, the core manager receives no snapshot and deletes nothing. If a space closed while Shepherdr was down, core's first published read removes its files.

---

## 6. Notifications

### 6.1 One subscription, one settings set

- The push subscription and `EventSettings` are unchanged: one set covers both sides. The `error` choice only fires for core, which matches today's behaviour.
- Settings copy depends on which sides are **present** in this browser's latest state. Settings apply to both, so wording them after the shown side would mislead (decision D3):

| Present | Intro | Opened / closed choices | `error` choice |
|---|---|---|---|
| TUI only, or none | Get an alert when a workspace needs attention or finishes. | Workspace opened / Workspace closed | hidden (keeps its value) |
| Core only | Get an alert when a space needs attention or finishes. | Space opened / Space closed | shown |
| Both | Get an alert when a **workspace or space** needs attention or finishes. | **Workspace or space opened** / **Workspace or space closed** | shown |

- The browser's `notifications.setHerdr(mode)` becomes `setPresent({tui, core})`. `eventsFromForm` keeps a hidden `error` value as it does today.

### 6.2 Evaluation per side

- `Manager.SetHerdr`, the single `m.evaluator` and the duplicate `m.herdr` field (a rereview simplicity note) are replaced by `Manager.Observer(backend) herdr.SnapshotObserver`, which returns a small observer with **its own** `snapshotEvaluator` (previous snapshot and baseline) and mutex. It stamps `Event.Herdr` itself.
  - Without this, the shared evaluator would diff a TUI snapshot against a core one and raise false opened, closed and status notices. This is the main correctness point.
- Baselines stay per side, as today: each projector's first publish after (re)connecting is a baseline. So a side starting, stopping or restarting never raises opened or closed notices for itself or the other side.
- Opened and closed come from each side's own `Snapshot.Workspaces`: TUI workspaces, and core document spaces (never **Other terminals**). The settings keys are shared.

### 6.3 Payload and wording

- The payload always has `"herdr": "tui" | "core"`. The TUI payload gains `"herdr":"tui"` (it was omitted before). `other_terminals` is unchanged (core only).
- Status destination: `/#terminal=<pane>&terminal_id=<id>&herdr=<side>`. Workspace and space notices still go to `/`.
- Service worker:
  - wording: `herdr === "core"` gives space or terminal words, otherwise workspace words (unchanged logic);
  - `statusDestinationMatches` also requires the link's `herdr` (missing means `tui`) to equal the payload's `herdr` (missing means `tui`).
  - An older cached service worker checks only `terminal` and `terminal_id`, so it still accepts the new links.
- A tap opens `#terminal=…&herdr=<side>`, so the Terminal page uses that side, whichever side Home shows:
  - TUI terminals open as today;
  - core terminals show **Terminal unavailable** until wave 2 (plan section 5, unchanged).

---

## 7. Browser

- **Switch placement:** directly under the masthead, above the attention bar and the tools row, because counts and the filter belong to the shown side.
  - It is a two-option segmented control, **Herdr TUI** | **Herdr Core**, in that order: two buttons with `aria-pressed`, in a group labelled "Herdr", with 44-px targets.
  - It is hidden when only one side is present.
  - It appears on Home, including the Blocked and Error views, but not on the Terminal page.
- **Per shown side:**
  - the filter placeholder (workspaces or spaces);
  - **N working**, **N blocked** and **N error** (error only for core, when nonzero);
  - Blocked and Error views;
  - New space and three-dot menus (TUI only, as today);
  - accessible names (`spaceWord(side)`).
- **Switching:** leaves the Blocked or Error view for **all terminals**, clears the filter and scrolls to the top. The two sides are different lists, so keeping a place makes no sense.
- **Masthead version line:** shows the shown side's version under the switch label's name: **Herdr TUI 0.9.5** or **Herdr Core 0.1.15**, whether or not the switch is visible. This replaces wave 1's "Herdr core 0.1.15" and changes TUI-only users' "Herdr 0.9.5" (decision D2b, approved). The user-visible core detail strings in `errors.go` ("Herdr core <v> is older…", "This Herdr core can't serve…") become "Herdr Core" for the same consistency.
- **Terminal page:**
  - uses `route.herdr`, then that side's Home and liveness (`liveActionsAvailable(side)`);
  - in 1b, a `core` route shows **Terminal unavailable** with **Home** (the existing wave 1 rendering), and a `tui` route behaves exactly as today;
  - **Home** from a terminal shows that terminal's side for this visit, so the row focus and scroll restore land (decision D4).
- **Rereview finding A**, fixed in 1b: in `home-view.ts` (the secondary line) and `home-model.ts` (`accessibleTerminalName`), an empty `agent.name` is treated as no name, as when it equals the title. A view-level test is like the existing "agent without a name" case.

---

## 8. Superseded wave 1 code to delete or replace

- `main.go`:
  - the `-herdr-core` flag;
  - `selectHerdrCoreSocket` and its three mode errors (replace with a plain "absolute path, or default, or not followed" helper);
  - `flagWasSet("herdr-core")` in the access-command check;
  - the `if *herdrCore {…} else {…}` wiring;
  - the two mode-specific ready lines;
  - the hard start error for a missing `herdr`;
  - `observeUploads` and its comment.
- `main_test.go`:
  - `TestHerdrCoreStartFlags`: delete it, and test the remaining path rules and the not-followed case;
  - `TestHerdrCoreStartKeepsFilesStagedForHerdrTUI`: rework it to run both projectors' publishes against one real manager and check that core publishes never touch a TUI record. Keep the TUI keep-and-remove cases.
- `internal/herdr/projector.go`: the `State.Herdr` JSON field and its `samePublishedState` comparison, and the Backend comment "chosen at start and never changes". Keep the `Backend` type for internal use.
- `internal/notifications/manager.go`: `SetHerdr`, `m.herdr` and the single evaluator, replaced by `Observer(backend)`.
- Browser:
  - `HomeState.herdr` and the `herdr` key in `home-parser.ts`;
  - the `herdr: "tui"` defaults in `main.ts`;
  - `notifications.setHerdr(state.herdr)`;
  - `main.ts:516` `if (state.herdr === "core")`, now route-based;
  - single-mode `core` checks in `home-view.ts`, now using the shown side.
- Docs: the README `-herdr-core` start section and flag row, and the CHANGELOG `-herdr-core` entry (section 9).

---

## 9. Proposed doc edits (not applied)

### 9.1 `docs/herdr-core-plan.md`

```diff
@@ header
-Status: **approved human direction and work brief** (2026-10-31). The human approved option A (keep the Herdr TUI backend unchanged and add a herdr-core backend chosen at start), decisions 1–10 below, decisions A–C in section 8 as recommended, and the documentation changes in section 5. Waves 1–2 are approved for dispatch; wave 3 needs separate approval.
+Status: **approved human direction and work brief** (2026-10-31). The human approved option A (keep the Herdr TUI backend unchanged and add a herdr-core backend), decisions 1–10 below, decisions A–C in section 8 as recommended, and the documentation changes in section 5. Later on 2026-10-31 the human replaced decision 1 and the section 2 flag rules: one Shepherdr follows both, and Home switches between them (section 2, "Both at once"). Waves 1, 1b and 2 are approved for dispatch; wave 3 needs separate approval.
+
+In this document, **core mode** means what Shepherdr shows for Herdr Core (its Home, terminals and notices), and **TUI mode** the same for the Herdr TUI. Both run at once in one Shepherdr.
@@
-Approved decisions 1–10: (1) keep the TUI backend and add a core backend chosen with an explicit start flag, no auto-detection; (2) …
+Approved decisions 1–10: (1) keep the TUI backend and add a core backend; one Shepherdr follows both on their sockets whenever each runs, and Home switches between **Herdr TUI** and **Herdr Core**, remembered per browser; (2) …
@@ ## 2. Start flags and connection
 ### Flags
 
-- `-herdr-core`: use herdr-core instead of the Herdr TUI for this start.
-- `-herdr-core-socket PATH`: the core socket. The default is `$XDG_RUNTIME_DIR/herdr-core/default.sock`. If `XDG_RUNTIME_DIR` is unset there is no default: the start fails with "-herdr-core-socket must be set when XDG_RUNTIME_DIR is not".
-- These are start errors:
-  - `-herdr-core` together with `-herdr-socket`;
-  - `-herdr-core-socket` without `-herdr-core`;
-  - a relative socket path.
-- In core mode Shepherdr does not look up the `herdr` binary. `main.go:201` currently fails without it.
+- There is no mode flag. Shepherdr follows the Herdr TUI (`-herdr-socket`, default unchanged) and herdr-core (`-herdr-core-socket`) at once, each whenever it is running.
+- `-herdr-core-socket PATH`: the default is `$XDG_RUNTIME_DIR/herdr-core/default.sock`. If `XDG_RUNTIME_DIR` is unset and the flag is not given, Shepherdr does not follow core: it logs this once at start and shows the Herdr TUI only.
+- A relative path in either socket flag is a start error.
+- `herdr` on `PATH` is needed only to open Herdr TUI terminals. Without it Shepherdr starts, logs a warning, and Herdr TUI terminals show **Terminal unavailable**.
 - Shepherdr never runs `hc connect` or `hc` itself, because those can start a core. It dials the socket only, so a stopped core shows honestly as "not running".
-- The ready log line adds `herdr="core"` and the socket path.
+- The ready log line names both sockets.
+
+### Both at once
+
+- Each Herdr has its own connection loop, state, Home, notification evaluator and upload records. Neither waits for or affects the other.
+- The Home message carries both complete states, `{epoch, tui, core}`, and is sent whenever either changes.
+- A Herdr is *present* once Shepherdr has had a Home from it since starting, or while it answers but can't be used. When both are present, Home shows a switch, **Herdr TUI** | **Herdr Core**. With one present, Home shows it without a switch. Each browser remembers its choice in local storage, and works for the visit without storage. A stopped Herdr stays in the switch with its last-known Home and **Herdr is not running** until Shepherdr restarts.
+- Every terminal link names its Herdr: `#terminal=<pane>&terminal_id=<id>&herdr=tui|core`. A link without `herdr` is a Herdr TUI link.
+- Notifications come from both, whichever one Home shows. Settings are one set for both.
@@ ### Connection states (existing badge words, unchanged)
+Each Herdr has its own badge state; Home shows the state of the Herdr it is showing. **Offline** applies to both.
@@ ### 3.2 Projection onto the existing Home contract
-The core read becomes the existing internal `Snapshot` / `Home` contract, so Home, the filter, Blocked, notifications and uploads keep working unchanged.
+The core read becomes the existing internal `Snapshot` / `Home` contract, so Home, the filter, Blocked, notifications and uploads work the same way for each Herdr.
@@ ### 4 table, Files row
-| Files (Reader Send, full-terminal Insert) | Unchanged local staging. Paths are sent as Message chunks. **Add files** shows only while the terminal has a non-null status. `terminal.put` is not used. |
+| Files (Reader Send, full-terminal Insert) | Unchanged local staging, with core records kept in their own file (`uploads-core.json`), apart from the Herdr TUI's. **Other terminals** keeps its files while the group is listed. Paths are sent as Message chunks. **Add files** shows only while the terminal has a non-null status. `terminal.put` is not used. |
@@ ## 5. User-facing copy in core mode
-| Masthead | Herdr 0.9.5 | **Herdr core 0.1.15** (the herdr-core version) |
+| Masthead | **Herdr TUI 0.9.5** | **Herdr Core 0.1.15** (the herdr-core version) |
+| Switch (both present) | **Herdr TUI** \| **Herdr Core** | same |
+| Notifications settings, both present | Get an alert when a **workspace or space** needs attention or finishes. **Workspace or space opened** / **Workspace or space closed** | same |
@@
-The browser learns the mode from one new field in the existing state JSON, `"herdr": "tui" | "core"`. The service worker gets the same field in its notification payload, so it can pick the space wording. A status notice for an agent under Other terminals also carries `"other_terminals": true`, so it can say terminal.
+The browser gets both Herdrs' states in each Home message (`tui`, `core`) and words Home for the one it shows. Every notification payload carries `"herdr": "tui" | "core"`, so the service worker can pick the wording. A status notice for an agent under Other terminals also carries `"other_terminals": true`, so it can say terminal.
@@ ## 6. What stays untouched in TUI mode
-- `-herdr-socket` and its default, the snapshot loop and the 0.9.0–0.9.5 gate.
+- `-herdr-socket` and its default, the snapshot loop and the 0.9.0–0.9.5 gate. (`herdr` on `PATH` is no longer a start requirement; see section 2.)
@@
-  - the `herdr` mode field;
+  - the two-Herdr Home message, the switch and `herdr` in terminal links;
@@ ## 9. Waves
+(Insert section 10.1 of this proposal as "### Wave 1b: both at once" after wave 1, and apply the wave 2 changes in 10.2.)
```

### 9.2 `docs/ui-direction.md`

```diff
@@ ## Voice
-… When Shepherdr is started for the Herdr app (herdr-core), say **space** and **tab** instead of workspace, matching the app. …
+… When Home shows **Herdr Core**, the runtime behind the Herdr app, say **space** and **tab** instead of workspace, matching the app. …
@@ ### With the Herdr app
+Shepherdr follows the Herdr TUI and Herdr Core together. When both are available, a switch at the top of Home, **Herdr TUI** | **Herdr Core**, picks which one Home shows, and each browser remembers its choice. With only one, Home shows it without a switch. Counts, **Blocked**, **Error** and the filter cover the Herdr that Home shows; the masthead names it with its version. Notifications come from both, and opening one goes to that Herdr's terminal.
+
 Home lists the app's spaces and tabs in the app's order …
```

D2b is approved, so also: `The normal masthead says **Shepherdr**` → `The normal masthead says **Shepherdr**, with the Herdr it shows and its version, such as **Herdr TUI 0.9.5**`.

### 9.3 `README.md`

```diff
@@ ## Install
-Shepherdr needs [Herdr](https://github.com/herdrdev/herdr) 0.9.0 or later running locally, `herdr` on `PATH`, and the same OS account as Herdr. Herdr 0.9.0–0.9.5 are validated; newer or unknown versions run best effort with a warning.
+Shepherdr needs [Herdr](https://github.com/herdrdev/herdr) 0.9.0 or later, or the Herdr desktop app, running locally under the same OS account. To open Herdr TUI terminals, `herdr` must be on `PATH`. Herdr 0.9.0–0.9.5 are validated; newer or unknown versions run best effort with a warning.
@@
-Using the Herdr desktop app instead? Shepherdr can show its spaces too. See [With the Herdr app](#with-the-herdr-app).
+Using the Herdr desktop app? Shepherdr shows its spaces too. See [With the Herdr app](#with-the-herdr-app).
@@ ### With the Herdr app
-If you use the Herdr desktop app, add `-herdr-core` to any start:
-
-```sh
-shepherdr -herdr-core
-```
-
-Home then lists the app's spaces and tabs, …
+Shepherdr also follows herdr-core, the runtime behind the Herdr desktop app, whenever it is running. There is nothing to add to the start. When the Herdr TUI and Herdr Core are both running, Home shows a switch between **Herdr TUI** and **Herdr Core**, and each browser remembers its choice. Notifications come from both.
+
+For Herdr Core, Home lists the app's spaces and tabs, …
@@
-With the app, Shepherdr shows Home and sends notifications. Opening a terminal is not available, and there are no space or terminal actions. `herdr` is not needed on `PATH`. herdr-core 0.1.15 is validated; newer versions run best effort with a warning.
+For Herdr Core, Shepherdr shows Home and sends notifications. Opening a terminal is not available yet, and there are no space or terminal actions. herdr-core 0.1.15 is validated; newer versions run best effort with a warning.
@@ ## Flags
-| `-herdr-core` | off | Use the Herdr desktop app's terminals instead of the Herdr TUI. Not saved. Cannot be combined with `-herdr-socket`. |
-| `-herdr-core-socket` | `$XDG_RUNTIME_DIR/herdr-core/default.sock` | Only with `-herdr-core`. Required when `XDG_RUNTIME_DIR` is unset. Absolute path. |
+| `-herdr-core-socket` | `$XDG_RUNTIME_DIR/herdr-core/default.sock` | herdr-core, behind the Herdr desktop app. Absolute path. Without it, when `XDG_RUNTIME_DIR` is unset, Shepherdr follows only the Herdr TUI. |
```

Wave 2 later removes "Opening a terminal is not available yet".

### 9.4 `CHANGELOG.md` `[Unreleased]`

```diff
 ### Added
 
-- Start with `-herdr-core` to use Shepherdr with the Herdr desktop app. Home lists the app's spaces and tabs with the app's names, and terminals that no space holds appear once under **Other terminals**. Home keeps working while the app window is closed.
+- Shepherdr also follows herdr-core, the runtime behind the Herdr desktop app, whenever it runs. When the Herdr TUI and Herdr Core are both running, a switch on Home picks which one to show, and each browser remembers its choice. For Herdr Core, Home lists the app's spaces and tabs with the app's names, and terminals that no space holds appear once under **Other terminals**. Home keeps working while the app window is closed.
+- Notifications come from both, whichever one Home shows.
 - With the Herdr app, an agent's **error** status shows on Home, **N error** opens an **Error** view, and a new **error** notification choice starts on.
 
+### Changed
+
+- `herdr` on `PATH` is needed only to open Herdr TUI terminals; Shepherdr starts without it.
+- The masthead names the Herdr it shows: **Herdr TUI 0.9.5** or **Herdr Core 0.1.15**.
+
 ### Compatibility
 
-- Support herdr-core 0.1.15 with the Herdr app. Newer versions run best effort with a warning. With the app, opening a terminal is not available, and there are no space or terminal actions.
+- Support herdr-core 0.1.15 with the Herdr app. Newer versions run best effort with a warning. For Herdr Core, opening a terminal is not available yet, and there are no space or terminal actions.
```

`AGENTS.md` and `north-star.md` need no change. Their "With the Herdr app, error appears too" still holds.

---

## 10. Waves

### 10.1 Wave 1b: both at once (one worker)

- **Base:** `e0e7446` (or the human's chosen integrated commit).
- **Scope:** sections 1–8 above, except the wave 2 parts (the core bridge, the core uploads manager, and `herdr` on terminal endpoints); rereview finding A; and the doc edits in section 9 once approved.
- **Likely files:**
  - `main.go`, `main_test.go`;
  - `internal/herdr/projector.go` (drop the `Herdr` JSON field; a fixed not-followed state), `errors.go` ("Herdr Core");
  - `internal/server/server.go`, `server_test.go` (two sources, the `{epoch, tui, core}` message), `file_uploads.go` and `workspace_actions.go` (TUI source wiring only);
  - `internal/notifications/manager.go`, `types.go` (`Observer(backend)`, destination `&herdr=`), `sender.go` (`herdr` always set), and their tests;
  - `web/src/home-parser.ts`, `home-model.ts` (two-side state, finding A), `main.ts` (shown side, storage, routes), `terminal-route.ts` (`herdr` parameter), `home-view.ts` (switch, masthead, cache reset, finding A), `notifications.ts` (presence-based copy), `service-worker.js` (`herdr` match), `style.css` (switch);
  - web tests, a `web/dist` rebuild, and the docs.
- **Unit tests worth having (small, no large stand-ins):**
  - two evaluators never cross-diff;
  - the TUI payload and destination carry `herdr=tui`;
  - the route parser handles a missing, `tui`, `core` and an unknown `herdr`;
  - the presence, settled and shown-side rules;
  - storage that throws;
  - the uploads test reworked (section 8);
  - finding A at view level.
- **Isolation for validation (required):** defaults now connect automatically, so **every** Shepherdr, `hc` and `herdr` process runs under `env -i`, with `XDG_CONFIG_HOME`, `XDG_RUNTIME_DIR`, `XDG_STATE_HOME`, `HC_SESSION`, `HERDR_APP_HOME` and `HERDR_RELEASES_URL=http://127.0.0.1:9` all pointing under `/tmp/shepherdr-core-w1b/`. Shepherdr is started **without** socket flags, so the defaults are exercised against isolated sockets. A second run passes both flags explicitly. Never touch the human's core, app, TUI or Shepherdr on 8787. Record the human's `terminals.json` hash before and after, as wave 1 did.
- **Acceptance gate** (production build; isolated herdr-core 0.1.15, isolated Herdr TUI 0.9.5; headless Chromium at a phone viewport, with a real phone where the human offers one):
  1. **Presence:**
     - both running: both are Live, and the switch shows **Herdr TUI** | **Herdr Core**;
     - TUI only, or core only: no switch, that side's Home;
     - neither: **Loading terminals**, then **Herdr is not running**, with no switch;
     - with both running, page loads show no pop-in of the switch (screenshots or a DOM-presence trace from the first frame).
  2. **Late start:** with Shepherdr running on the TUI only, start core. The switch appears with Herdr TUI still selected, and the page doesn't move. Repeat the other way round.
  3. **Independence:**
     - `hc server stop` shows Core as **Herdr is not running** with its last-known Home; the switch stays; TUI stays Live with no Reconnecting frame (log of TUI states).
     - `hc server restart`: Core goes Reconnecting, then Live.
     - Stop and restart the Herdr TUI: the mirror case.
     - A stand-in core older than 0.1.15 shows **Cannot use this Herdr** with its detail on the Core side while the TUI is Live.
  4. **Per browser:**
     - two browser profiles pick different sides, and each keeps its choice across reloads;
     - with storage disabled or throwing, the switch works for the visit and a reload falls back to Herdr Core when both are present;
     - switching from Blocked returns to all terminals of the other side, with the filter cleared.
  5. **Words per side:**
     - filter copy, masthead text, **New space** and menus on TUI only;
     - **N error** and the Error view on core only;
     - accessible names say workspace or space.
  6. **Notifications from both:**
     - while Home shows Core, a TUI agent goes blocked, and the candidate and payload carry `herdr=tui` with workspace wording; the mirror case for core, with space wording; an Other terminals agent says terminal;
     - starting, stopping and restarting either side raises no opened or closed candidates;
     - one settings set; the settings copy follows section 6.1 for TUI-only, core-only and both.
  7. **Taps:**
     - a TUI status link opens the real TUI Terminal;
     - a core status link shows **Terminal unavailable** with **Home**;
     - an old-format TUI link (no `herdr`) opens the TUI Terminal;
     - **Home** from a core terminal shows Core Home with the row focused.
  8. **Uploads:**
     - stage a file for TUI `w1` (`StageAndForward`, as earlier runs did);
     - run both sides, with core reads, a core restart, a core stop and a Shepherdr restart: the file and record are kept;
     - close `w1`: they are removed.
  9. **Flags:**
     - `-herdr-core` is refused as an unknown flag;
     - a relative socket path is refused;
     - with `XDG_RUNTIME_DIR` unset and no core flag, Shepherdr starts, logs one info line and shows the TUI only;
     - with `herdr` not on `PATH`, Shepherdr starts, logs a warning, core Home works, and a TUI row opens **Terminal unavailable**.
  10. **Finding A:** a real program in an Other terminal runs `printf '\e]0;\a\e]7501;state=idle\a'`. Its picker row has no stray "·", and its accessible name has no empty part.
  11. **TUI unchanged:** the existing Go and web tests pass; `TestRealProductionHome` and `TestRealHerdrSnapshot` pass against the isolated TUI; TUI Terminal, Message, Send keys, actions and file send work with core also running.
  12. **Hostile values** on both sides render as inert text, including in the switch region (the labels are fixed text).
- **Gate for wave 2:** an independent review approves 1b, and the integrator's run of checks 1, 3, 6, 7 and 11 passes on the integrated commit.

### 10.2 Wave 2 (Terminal on core): what changes

- **Base:** the approved integrated 1b commit.
- **Added scope:**
  - `/api/terminal`, `/api/terminal/read` and `/api/terminal/files` take a required `herdr` parameter. The server routes to the TUI bridge or the new core bridge, each checking targets against its own projector.
  - Terminal routes are registered when either bridge exists. The core bridge needs no `herdr` binary.
  - The second upload manager (`uploads-core.json`), wired only to the core projector, with the **Other terminals** rule (D5). File-send agent presence for core = a non-null status, as plan 4 already says. `Status.Valid()` currently rejects `error`; the review note stands.
  - Core's `can_open_terminals` (section 2) becomes true when the core bridge exists, as the TUI's already is with its bridge.
  - README and CHANGELOG drop "not available yet".
- **Added acceptance:**
  - With both running, a TUI terminal and a core terminal are open in two tabs at once, each controlling its own. Notification taps from each side open the right one.
  - Staged files for a TUI workspace and a core space are kept and removed independently: closing the core space removes only core files, and the reverse.
  - Other terminals files stay while the group is listed, survive a Shepherdr restart while it still is, and are removed once it's gone.
  - The existing wave 2 checks apply to core; TUI Terminal acceptance is unchanged.
- **Release:** still not before wave 2 is integrated and the human has accepted both side by side on a real phone.

---

## 11. Risks

- **Auto-follow exposes core to every upgrader.** Anyone running herdr-core gets Herdr Core on Home with no flag, so an unfinished core must never ship. No release from `master` between 1b and wave 2 (D6).
- **Validation can touch the human's sessions.** Defaults now connect to the human's real TUI and core, read-only but real. A worker who forgets `env -i` and the isolated XDG folders would read them. The brief must make isolation mandatory and evidence-checked (environment dumps, the before/after hash).
- **Shared evaluator bug.** If per-side evaluators are missed, the result is false notices. The test in 10.1 must fail when the change is reverted.
- **A stopped side stays in the switch** until Shepherdr restarts (D2). This is honest (**Herdr is not running**), but it lingers if someone uninstalls one Herdr.
- **Pages left open across the upgrade** get a Home frame they can't parse and stay on **Offline** until reloaded. This already happened with wave 1's `herdr` field. No reload machinery is proposed.
- **Load wait:** a hung Herdr on its first attempt holds **Loading terminals** for up to about 5 s (the existing request timeout) before the other side shows.
- **Notification volume** can double for people running both.
- **A missing `herdr`** is now a log warning instead of a start error. A TUI user with a broken `PATH` finds out only when opening a terminal.

---

## 12. Decisions (approved)

- **D1. First side when both are present and nothing is remembered:** **Herdr Core** (the human's choice).
- **D2. A side that stops after being seen:**
  - **(a) approved:** it stays in the switch with its last-known Home and **Herdr is not running** until Shepherdr restarts;
  - (b) hide it when it stops. The switch then flickers on stop and start, and the page jumps if it was being shown.
- **D2b. Masthead text:**
  - **(a) approved:** always **Herdr TUI 0.9.5** / **Herdr Core 0.1.15**. This changes the TUI-only "Herdr 0.9.5".
  - (b) keep "Herdr 0.9.5" for the TUI and use **Herdr Core 0.1.15** for core.
- **D3. Notification settings copy with both present:** "workspace or space" (section 6.1), approved; or words that follow the side Home shows (shorter, but misleading, since settings cover both).
- **D4. Home after a terminal from the other side:** shows that terminal's side for this visit, without changing the remembered choice (approved); or always the remembered side, in which case the row focus is lost.
- **D5. Files for Other terminals (wave 2):** they live as long as the group is listed (approved); or hide **Add files** under Other terminals.
- **D6. Release hold (approved):** no release from `master` between 1b and wave 2. Any urgent TUI-only patch would be cut from the `v0.4.3` line.

Not proposed, and not to be added without a new decision: attention counts or dots for the other side on the switch, a combined Home, server-side selection, or per-side notification settings.
