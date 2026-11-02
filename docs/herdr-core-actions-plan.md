# Herdr Core actions plan

Status: **approved human direction and work brief for waves 3a and 3b** (2026-10-31). The human approved D1 (a), D2 (a), D3, D4, D5 (a), D6 and the new copy in section 9, and the doc changes in section 5. Wave 3a, then wave 3b, are approved for dispatch.

Note (wave 3c): New space and New terminal while the app window is closed, through a labelled `terminal.run`, are planned in `docs/herdr-core-closed-create-plan.md`. That plan supersedes this one's "only while the window is open" for those two actions; Rename space is unchanged.

Planning base: Shepherdr `06c40b1` (master). Upstream: herdr-core 0.1.15 (the human's installed `hc`, copied), Herdr app 0.1.10 (the human's installed binary, copied; source `e3cdccc`).
Date: 2026-10-31. Validation evidence was recorded outside the repository during planning.

The human's request (2026-10-31): "Everything. Parity with what we had for TUI or whatever equivalents core offers."

---

## 0. Summary

- **Two routes exist, and they behave differently.**
  - **Core API** (`terminal.kill`): works whether the app window is open or closed. Killing a terminal makes an open app remove its pane, then its tab, then its space. A closed app tidies up on its next start. Home is right in both cases, because the approved projection already skips dead terminals and empty tabs and spaces.
  - **The app's documented command interface** (`herdr-app do <command>`): gives exact equivalents for New space, Split and Rename space. It works only while the app window is open, and on Linux closing the window quits the app.
- **Core alone cannot do the structure actions properly.** A core `terminal.run` with `herdr.space`/`herdr.tab` labels:
  - can't name a space;
  - can't choose where a split goes;
  - can't be placed while the window is closed: until the app opens, the terminal sits under **Other terminals**;
  - **brings back a space the person just closed** if Shepherdr's ids are stale (validated, R1).

  I don't recommend it for any action.
- **Worktrees have no route.** The app's worktree mod has no command that `herdr-app do` can reach (validated). The only route is Shepherdr running Git itself, so it needs a human decision (D1).
- **Rename terminal has no equivalent.** Core names are set only at start, and the app has no terminal or pane rename. The nearest equivalent the app offers is **Rename space** (D4).
- **Recommended, in two waves of one worker each:**
  - **Wave 3a, core API only:** **Close terminal** and **Close space**, both through `terminal.kill`. They are available whenever core Home is live, window open or closed. They need no app interface and no new Herdr method beyond the already-planned `terminal.kill`.
  - **Wave 3b, the app's command interface, only if D2 is approved:** **New space**, **New terminal** (split) and **Rename space**. They are shown only while the app window answers `herdr-app status`.

---

## 1. Interfaces found

### 1.1 Core API (published, `hc schema`)

- `terminal.kill {terminal}`: "hangup, then terminate, then kill. Returns the observed result."
  - It is synchronous. A shell ended in ≤1 s, and so did a program ignoring HUP and TERM (`signal: 9`).
  - On a kept terminal, an explicit kill ends it with `reason: killed` and no restart.
  - Killing an **already finished** terminal returns its finished record with no error. So "it was live" must come from Shepherdr's fresh read before the kill, not from the reply.
  - Unknown address: `not_found`. Names can't be shaped like addresses (`bad name "t2" … not shaped like an id`), so a hostile name can't redirect a kill.
- `terminal.run {cwd, labels:{herdr.space, herdr.tab}}` opts a terminal into the window (the app skill documents this). Its behaviour is recorded in section 2, and it is not recommended.
- `doc.set` stays forbidden.
- **Core does not reveal whether the app window is open.** `server.status` lists holders, not clients.

### 1.2 The app's command interface

| What | Documented? | Notes |
|---|---|---|
| `herdr-app do <command> [--k v … \| one JSON object]` | **Yes:** `herdr-app --help`, `--skill`, `docs/commands.md` | Prints the reply JSON. Exit codes: 0 ok; 1 the app replied `{"error"}`; 2 no window, or "the window hung up", or a usage error. |
| `herdr-app commands` | Yes | Prints the command list **without a window**, so it can gate arguments. |
| `herdr-app status`, `herdr-app ls` | Yes: "talk to the running window, not a new app" | With no window: exit 2, stderr `no Herdr window at <home>/herdr-app.sock: …`. About 5 ms per call (10 calls in 50 ms). |
| `herdr-app --version` | Yes | `herdr-app 0.1.10` |
| Bare `herdr-app` | Yes: **opens or raises the window** | Shepherdr must never run it. Only `do`, `status`, `commands` and `--version` go to the client or print and exit (`src/main.rs:88-101`). |
| `$HERDR_APP_HOME/herdr-app.sock` (default `~/.config/herdr-app/herdr-app.sock`) | **No.** Only a source comment (`src/control.rs:1-3`: "one JSON line each way") and the path inside the CLI's error message | Protocol: `{"do":cmd,"args":{…}}` → one reply line. The window has 5 s to answer, else `{"error":"the window did not answer"}`. |

**Whether the window is open.** The socket **file stays after the window closes**: connecting gives `Connection refused` (validated, B9). The human's own `~/.config/herdr-app/herdr-app.sock` existed this morning with no app running. So file existence means nothing. The window is open exactly when `herdr-app status` exits 0, which is the same as a successful connect.

**Agents vs the person.** `do` runs as an agent (`person=false`, `window.rs:3416`):
- `space.close` without `confirmed:true` is refused: `space.close ends its terminals: pass confirmed: true`.
- A rename without `name` is refused rather than opening a dialog.
- Unknown argument names are refused rather than falling back to the focused pane or space.

**Reply meaning.**
- `space.new` → `{space, tab, terminal}`; `pane.split` → `{pane, terminal}`. These are the app's own confirmation.
- `space.close`, `terminal.kill`, renames → `{"ok":true}`.
- The app's kills run **after** the reply (`window.rs:2470-2505`, detached). Measured: the document changed 0–6 ms after the reply, and the terminal finished about 1 s after.

**Argument parsing trap.** In the `--key value` form, any value that parses as JSON becomes JSON (`control.rs` `parse_args`), so a name like `123` turns into a number. Shepherdr must always pass **one JSON object** argument, built by encoding, never by shell.

### 1.3 Worktrees

- The worktree mod (`defaults/mods/worktree/main.js`) is a dialog. It opens only through the menu mod's `openOverlay` with props `{space, action}`.
- It registers no `<mod>.<command>`:
  - `herdr-app do worktree.new` → `unknown command`;
  - `mod.open` takes only `name` → `mod.open: unknown argument(s): props`.
- `herdr-app do mod.open --name worktree` opens an untargeted, person-facing dialog in the window. That is not a usable route.
- The mod itself runs `git worktree add/remove` through core `exec` and then `space.new` / `space.close`. Its **Remove worktree uses `--force` when the checkout is dirty**, after its own warning. That is the opposite of Shepherdr's never-force rule.

**There is no route for Shepherdr except running Git itself.** See D1.

---

## 2. What the app did (validated on an isolated core and an isolated app)

The setup was a fresh core session `act` (0.1.15) and a fresh app home (0.1.10), on the desktop session through an absolute Wayland path. The cases are labelled A (window closed), B (window open), K (workers), R (races) and W (worktrees).

| # | Case | Result |
|---|---|---|
| A1/A2 | Window **closed**. Core `terminal.run` with `herdr.space=spshepA` (new id); a second with the same space and `herdr.tab=tbshepA2`. | The document stayed at version 0, so in Shepherdr both are under **Other terminals**. On app start, both were adopted into **one unnamed space** (`spshepA`, named after its first terminal's folder). The labelled tab kept its id. The unlabelled one got a random `tb…` id and no `herdr.tab` label. `keep` was turned on. |
| B1 | Window open. Core run into an existing space and tab. | It was split **right of the tab's last pane**, not beside a chosen terminal. The window's shown space and focus did not change. |
| B2 | Core run with only an existing space label. | A new unnamed tab, inserted after the shown tab. The other tabs' numbers shifted (`plain / 2` → `plain / 3`). |
| B3 | Core run with a new space id. | A new unnamed space, named after its git top folder (`repo`). The window's view did not change. |
| B4 | `do space.new {name:"api work", folder, tab:"main"}` | Reply `{space, tab, terminal}`. A named space and tab appeared, and **the window switched to the new space**. |
| B4b/c | `space.new` with a missing folder, or with `~` | `{"error":"cannot start a terminal: … No such file or directory … (check the program name and cwd)"}`, exit 1. **The app does not expand `~`**; Shepherdr already does. Each failure used up a core address. |
| B4d | Hostile space name `<img src=x onerror=alert(1)> / x` | Stored literally. `where` becomes `<img …> / x / 1`, which is harmless because named spaces use the document name. Must render as text (already a Home acceptance check). |
| B5 | `do pane.split {direction:"right", pane:<t2's pane>}` while another space is shown | Reply `{pane, terminal}`. Inserted directly right of t2. The new terminal starts in t2's folder. The window's view and focus did not change. |
| R8 | The same split **while that tab is shown** | **The window's focus moved to the new pane.** Someone typing in the app would now be typing into it. |
| B5c | Split with an unknown pane | `{"error":"no pane \"pNOPE\""}`, exit 1. |
| B6 | `do space.rename {id, name}`, `tab.rename {id, name}` | `{"ok":true}`; `labels` then `doc` events; `where` updated. Without `name` → refused (`space.rename needs name (empty: automatic)`). |
| B7a | Core kill of one terminal in a three-way split | The pane was removed. Events: `finished`, then `doc`. |
| B7b | Core kill of a **tab's last terminal** (the space has other tabs) | **The tab was removed**, with no dialog. The remaining tabs were renumbered (labels events). |
| B7c | Core kill of a **space's last terminal** | **The space was removed**, with no dialog. |
| B8 | `do space.close {id, confirmed:true}`; `do terminal.kill` | `{"ok":true}` in about 6 ms. The document changed at once, and the terminals were finished about 1 s later. A stale id gives `no space "…"` / `no running terminal "t99"`, exit 1. |
| B9 | Window closed (`do window.close`, which quits on Linux) | The socket file remains and connect is refused. Core kill of a tab's last terminal: **the document is not updated** (the dead block stays). Core run into an existing tab: no `where` label, not in the document. |
| B9c | Reopen the app | The dead pane and its tab were removed; the run-while-closed terminal was adopted into its labelled tab. |
| K1 | Core kill of a parent with a worker (`parent` set, default) | The worker ended too (`die_with_parent`). |
| K2 | App `terminal.kill` of a parent whose worker has `outlive_parent` | The worker ended too: **the app kills the whole tree** except kept sub-agents (`window.rs:2468`). Core kill would spare an `outlive_parent` child. |
| R1 | The app creates space X, the app closes X, then **core run labelled with X's old ids** | **X came back**, unnamed (the name "doomed" was lost). A core route with stale ids resurrects closed spaces. The app route refuses stale ids instead. |
| W1–W4 | Worktree through `do` | No route (section 1.3). |

Races with the person, in summary:
- Shepherdr never writes the document, so "newest write wins" can't lose the person's layout.
- App commands run one at a time on the app's UI thread. A stale id gives a definite error.
- What does race:
  - (a) app New space and Split **change what the window shows or focuses** (B4, R8);
  - (b) core runs with stale ids resurrect spaces (R1);
  - (c) the window can close between Shepherdr's check and its command. That gives `no Herdr window` (not sent), or "the window hung up" / "did not answer" (unknown).

---

## 3. Parity map

"Window" means the app window must be open.

| TUI action (Shepherdr today) | Core / app equivalent | Window | Proposed |
|---|---|---|---|
| **New space** (`workspace.create {cwd, label}`) | App `space.new {folder, name}`. The core alternative (`terminal.run` with a fresh `herdr.space`) is unnamed and lands in **Other terminals** while the window is closed. | yes | **Wave 3b**, app route only, shown while the window is open. |
| **New worktree** (`worktree.create`) | None. Only Git through core `exec` (what the app's mod does). | n/a | **Hidden** (D1). |
| **Delete checkout** (`worktree.remove`, never force) | None. The app's own Remove worktree forces when dirty. | n/a | **Hidden** (D1). |
| **Close workspace** (`workspace.close`) | (a) Core: kill each live terminal Home shows in the space. (b) App `space.close {id, confirmed:true}`. | (a) no, (b) yes | **Close space, wave 3a**, core route (D5). |
| **Close workspace** for a group (`close_group`, linked workspaces) | No linked-workspace relation in core. | n/a | Does not map. Core never offers it, and its confirmations never name linked workspaces. |
| **Split terminal** (`pane.split`, Beside/Below, opens the new terminal) | App `pane.split {direction:"right"\|"down", pane:<document block id>}`. Core labels can only append right of the tab's last pane. | yes | **Wave 3b**, app route, for terminals placed in the document only (workers have no pane). |
| **Rename terminal** (`pane.rename`) | None (core name is fixed at start; no app rename). Nearest: app `space.rename` / `tab.rename`. | yes | **Hidden.** Add **Rename space** (wave 3b) only if D4 is approved. |
| **Close terminal** (`pane.close`; closes the workspace when it is the last) | Core `terminal.kill {terminal: address}`. The app removes the pane, tab or space itself. | no | **Wave 3a.** |
| Three-dot menus and confirmations | Built from `actions` arrays, as today | | Same machinery, with space words (section 4.4). |
| Refresh after an action | Core events (`finished`, `doc`) plus one `RequestRefresh` | | Section 4.5. |
| Expand all / Collapse all, worktree groups | n/a | | Unchanged (hidden). |
| Take over | n/a | | Unchanged (approved). |

---

## 4. Proposed design

### 4.1 Which actions, and when

Core Home's `actions` arrays become non-empty only as below. Everything else in plan section 3.5 stays hidden.

| Action | Where | Shown when |
|---|---|---|
| **Close terminal** | the terminal's three-dot menu (picker, the merged menu of a one-terminal space row, or a one-terminal tab row in a space with several tabs) | core Home is live. Rows under **Other terminals** follow D6. |
| **Close space** | the space row's three-dot menu, or the merged menu | core Home is live and the space is a document space (never **Other terminals**) |
| **New space** (3b) | tools row | core Home is live **and** the app window is open **and** the app gate passes (4.3) |
| **New terminal** (split, 3b) | the terminal's menu | as for New space, and the terminal is placed by the document (a pane exists) |
| **Rename space** (3b, D4) | the space menu | as for New space |

"Live" is the existing `liveActionsAvailable` rule (Live, not last-known, current access), applied to the shown side.

### 4.2 Close terminal and Close space (wave 3a, core API only)

The existing two-step flow is reused: **prepare** (a fresh complete core read, then facts), **confirm**, then **run** (a fresh complete read; the facts must still match, else `stale`; then mutate). There is one action at a time per side.

**Close terminal: target and facts.**
- Target: `{workspace_id: space id, tab_id, pane_id: address, terminal_id: core.id:address}`. It must resolve exactly in a fresh core projection: same space, same tab, address live, `terminal_id` equal.
- `closes_tab`: no other terminal Home shows in that tab would remain. "Remain" means what Home would show after the kill: the target's workers that end with it (`die_with_parent: true` down the chain) go, and a worker that outlives its placed parent moves to **Other terminals**, so it keeps nothing alive.
- If none would remain in the **space**, the facts carry the space's close facts, and the confirmation is the Close space one. This is the TUI's last-pane rule.
- Workers that end with it are counted (see the copy in 4.4).

**Close terminal: mutation.** One `terminal.kill {terminal: address}`. The address only, never a name.

**Close space: mutation.**
- Kill every live terminal that Home shows in the space, workers included (one `terminal.kill` each, one after another). Workers go first, deepest first across the space, so each ends before the terminal that started it; otherwise the order is Home order. Only terminals Home shows in this space are killed: a sub-agent the app placed in another space, or one under **Other terminals**, is left running unless it ends with its parent (`die_with_parent`), which core decides.
- A terminal that has already ended, on its own or with its parent, is skipped when the next fresh check finds it finished. Kill returns finished records anyway, so this is harmless.

**Close space: confirmation.** It reuses the workspace confirmation facts: `workspace_label`, `scope_workspace_ids: [space id]`, `agent_total`, `interruption_counts`. In core mode the counts add `error`; the Herdr TUI always sends 0 for it.

**Confirmation of the result.**

| Action | **succeeded** when | Otherwise |
|---|---|---|
| Close terminal | the kill reply's `state` is `finished` | — |
| Close space | every kill reply is `finished`, and one complete core read afterwards shows the space with no live terminal in it | **unknown**. The person may have added a terminal in the app meanwhile. |

How errors map:

| What happened | Outcome |
|---|---|
| Dial failure before anything was written | refused, `herdr_unavailable` (not sent) |
| JSON-RPC `not_found` on the first kill | refused, `not_found` |
| Another JSON-RPC error before any kill took effect | refused, `herdr_refused` with the message |
| Write succeeded but the read failed or timed out; or any error after a kill had taken effect | **unknown** |

There is no retry.

**What the app does** (validated): when the window is open, it removes the pane, then the tab, then the space with no dialog (B7). When the window is closed, Home is already right through projection rules 1 and 4, and the app tidies up on its next start (B9c).

### 4.3 App actions (wave 3b, if D2 is approved)

**App client.**
- Found once at start as `herdr-app` on `PATH`, like `herdr`. If it is missing, log once ("Herdr app actions are off: herdr-app is not on PATH") and show no app actions.
- It runs only `herdr-app --version`, `herdr-app commands`, `herdr-app status` and `herdr-app do <command> <one JSON object>`, with `exec.Command` (no shell), Shepherdr's own environment, and a 10 s timeout (2 s for `status`).
- **It never runs bare `herdr-app`.**

**Gate.**
- At start: `--version` and `commands`.
- Validated: 0.1.10 through 0.1.11. Newer versions run best effort with a warning, like core.
- Required commands and arguments:
  - `space.new {name, folder}`;
  - `pane.split {direction, pane}`;
  - `space.rename {id, name}` (D4).
- A missing command or argument turns app actions off and logs one line.

**Pairing.** The app at its default home drives the default core session. App actions are offered only while Shepherdr follows the default core socket (`-herdr-core-socket` not given). With a custom socket they are off, with one log line.

**Window open.**
- A watcher outside the core loop asks `herdr-app status` every 5 s, and again right before each app action, so a slow or hung window never delays core Home.
- Exit 0 means open. Exit 2 with stderr starting `no Herdr window at` means closed.
- A change triggers the normal complete core read and publish, so the actions appear or disappear in one complete Home. There is no new visible state.

**Commands.**
- **New space:** `do space.new {"folder": <absolute, ~ expanded by Shepherdr>, "name": <label, omitted when blank>}`. No `tab`, `argv`, `task` or `machine`. Success is `{space, tab, terminal}`. Before sending, Shepherdr checks the folder exists on this machine and otherwise refuses with "This folder doesn't exist."; a folder that vanishes after the check falls back to the app's own refusal.
- **New terminal (split):** `do pane.split {"direction": "right"|"down", "pane": <document block id>}`.
  - The document block id (`p…`) is kept server-side with each placed terminal. It is never sent to the browser.
  - Success is `{pane, terminal}`.
  - Shepherdr then waits, at most the request timeout (5 s), for the core projector's **published** Home to contain the new address in the same tab. The `doc` event follows within milliseconds (B5, timing). Then it answers `succeeded` with the target, and the browser opens it as the TUI flow does. If the wait runs out, the outcome is **unknown**. This adds no new reads.
- **Rename space:** `do space.rename {"id": <space id>, "name": <text; empty = automatic>}`. Success is `ok`.

**Reply classification.**

| CLI result | Outcome |
|---|---|
| exit 0 with the expected keys | succeeded |
| exit 1 and `{"error":"the window did not answer"}` | **unknown**: the command may still run |
| exit 1 with any other `error` | refused `herdr_refused`, with the app's message as detail |
| exit 2 and `no Herdr window at…` | refused `app_closed` (not sent), and Home re-reads |
| exit 2 otherwise ("the window hung up"), a timeout, a kill, or unparsable output | **unknown** |

The `herdr_refused` errors are definite: validation, `no space`, `no pane`, or a spawn failure (a missing folder is normally caught by Shepherdr's own check first).

There is no retry, and Shepherdr never sends `confirmed` for anything except `space.close`, which the 3b design doesn't use.

### 4.4 Menus, confirmations and copy (TUI copy with space words)

| Where | TUI | Core mode |
|---|---|---|
| Space menu items | New worktree, Close workspace, Delete checkout | **Rename space** (3b, D4), **Close space** |
| Terminal menu items | New terminal, Rename terminal, Close terminal | **New terminal** (3b), **Close terminal** |
| Tools row | New space | **New space** (3b, window open) |
| Close confirmation heading / button | Close {workspace}? / Close workspace | Close {space}? / **Close space** |
| Close copy | "This closes this workspace. N agents will be affected. Its terminals will end, and unsaved work can be lost. The folder and branch remain." | "This closes this **space**. N agents will be affected. Its terminals will end, and unsaved work can be lost. The folder and branch remain." Never mentions linked workspaces. |
| Interruption warning | working, blocked, unknown | working, blocked, **error** |
| Close terminal copy | "Closing this terminal stops what is running there. {Agent} is {status}. The {tab} tab will also close." | the same, plus **"N terminals it started will also close."** when workers end with it (new sentence, needs approval) |
| Working headings | Checking workspace / Closing workspace | **Checking space / Closing space** |
| Results | Workspace closed: "Its folder and branch remain. Home will update when it is ready." (via Close terminal: "Its terminals are closed. The folder and branch remain.") | **Space closed**: "Its folder and branch remain. Home will update when it is ready." in both cases, because a Close space through the app finishes its kills after the reply |
| Refusals | "This workspace is no longer here." / "This workspace changed. Open the action again to check the latest details." / "Another workspace action is running. Try again when it finishes." | **space** in place of workspace; a Close terminal whose terminal is gone says "This terminal is no longer here."; New space with a missing folder says "This folder doesn't exist." |
| New refusal (3b) | — | `app_closed`: **"The Herdr app window is closed. Open it on the computer, then try again."** |
| New space sheet | "Add a workspace for an existing folder." / Working directory / Label (optional) / Create space; suggestions from repository checkouts | "Add a **space** for an existing folder." The fields and button are unchanged. Suggestions are the repository folders open in Herdr TUI or Herdr Core: for each space Home shows, the Git top folder of its app `folder` when that is a top-level repository (not a linked worktree or submodule), found by read-only file checks. |
| New space result | Space created: "Home will update when the new space is ready." | unchanged |
| Split sheet | Split terminal: "This splits {terminal} and opens the new terminal." Beside / Below / Cancel | unchanged |
| Rename space sheet (D4) | (Rename terminal: Name; "Leave the name blank to use the automatic name.") | **Rename {space}**: Name; the same help text; **Rename space**; result **Space renamed**: "The new name is ready on Home." |
| Unknown results | "Result unknown. Check Home before …" | unchanged, with space words |

Accessible names follow the same words ("Actions for {space}").

### 4.5 Outcomes and the refresh

- Outcomes are **succeeded**, **refused** (with a reason) and **unknown**, as today. There is no automatic retry anywhere.
- After every succeeded or unknown outcome, the server calls `RequestRefresh()` on the **core** projector: one complete read, under the existing one-read latch. Core's own `finished`, `doc` and `labels` events also trigger reads, and the latch absorbs them.
- Home changes only when a complete read shows the change. Shepherdr shows no "pending" rows.
- Each side has its own coordinator and its own "one action at a time". TUI actions and core actions never block each other, matching "neither waits for or affects the other".
- Browser contract: the action requests gain a `herdr` field, `tui` or `core`, which both builds send. The server routes it to that side's coordinator, and refuses `invalid_request` when that side has no actions. The response shapes are unchanged except for `error` in `interruption_counts`. `openCreatedTerminal` uses the side from the request instead of the hard-coded `"tui"`.

### 4.6 TUI concepts that don't map

| TUI concept | Core mode |
|---|---|
| Linked workspaces, Close group | No relation exists. Close space affects only that space. |
| New worktree, Delete checkout | Hidden (D1). |
| `checkout_path` and its suggestions | A space's repository folder. New space suggests both Herdrs' folders. |
| Rename terminal (manual name) | Hidden. Rename space only if D4 is approved. |
| `focus:false` on create and split | The app has no such option. New space and Split change the window's view or focus (D3). |
| A refused dirty checkout | n/a |
| The pane rectangle check | n/a: core targets are address plus `core.id`, plus the document block id for split |

---

## 5. Proposed doc diffs (proposals only; nothing edited)

### 5.1 `docs/herdr-core-plan.md`

Status line:

```diff
-… Waves 1, 1b and 2 are approved for dispatch; wave 3 needs separate approval.
+… Waves 1, 1b and 2 are approved for dispatch. Wave 3 (actions) is planned in two parts, 3a and 3b; each needs separate approval.
```

Header note (line 11):

```diff
-Close terminal, New space and Close space stay out of waves 1–2.
+Actions in core mode come in wave 3 (section 3.5 and section 9).
```

Section 2, "Version gate and required methods":

```diff
-  - wave 3, only if approved, adds `terminal.kill`.
-- Never used: `terminal.read` (it scrolls full-screen apps), `terminal.send`, `doc.set`, `terminal.run`, `terminal.put`, `machine.*`, `server.*`, `exec`/`file.*`.
+  - wave 3a adds `terminal.kill`.
+- Never used: `terminal.read` (it scrolls full-screen apps), `terminal.send`, `doc.set`, `terminal.run` (a labelled run can't name a space, can't place a split, and brings back a space closed a moment earlier), `terminal.put`, `machine.*`, `server.*`, `exec`/`file.*`.
```

Section 3.2 table row:

```diff
-| Terminal and space `actions` | empty in waves 1–2 |
+| Terminal and space `actions` | as section 3.5 |
```

Section 3.5, replaced:

```markdown
### 3.5 Actions in core mode

Shown, through core, whenever core Home is live (the app window open or closed):
- **Close terminal**: `terminal.kill` on the exact address. The confirmation is the TUI's, plus "N terminals it started will also close." when workers end with it. If nothing else Home shows would remain in the space, the confirmation is Close space's, and confirming closes everything shown in the space, as the TUI does for a last pane.
- **Close space**: `terminal.kill` on every live terminal Home shows in the space, workers included. The confirmation is the TUI's Close workspace one in space words, never naming linked workspaces, with `error` in the interruption warning. **Other terminals** has no space actions.

Shown only while the Herdr app window is open (wave 3b; `herdr-app do`, see section 4b):
- **New space**: `space.new` with the folder (`~` expanded by Shepherdr) and the optional label as its name;
- **New terminal**: `pane.split` beside or below an exact terminal that has a pane;
- **Rename space**: `space.rename` (if approved).

Hidden in core mode:
- worktree groups, **New worktree** and **Delete checkout** (core and the app expose no worktree command);
- **Rename terminal** (core names are fixed at start, and the app has no terminal rename);
- **Take over** and **Take over and send**;
- every three-dot menu with no actions.

Expand all and Collapse all only apply to worktree groups, so they never show.
```

New section 4b, "App window actions" (wave 3b only):

```markdown
## 4b. App window actions (wave 3b)

- Shepherdr runs `herdr-app` from `PATH` only as `--version`, `commands`, `status` and `do <command> <one JSON object>`, with no shell. It never runs bare `herdr-app`, which opens the window. If `herdr-app` is missing, or Shepherdr follows a non-default core socket, app actions are off and this is logged once.
- Gate: herdr-app 0.1.10 through 0.1.11 validated; newer runs best effort with a warning. `commands` must list `space.new {name, folder}`, `pane.split {direction, pane}` and `space.rename {id, name}`.
- Window open = `herdr-app status` exits 0 within 2 s. It is checked every 5 s and before each app action. A change publishes one complete Home.
- Results: exit 0 = succeeded; "the window did not answer", "the window hung up", or a timeout = unknown; "no Herdr window" = not sent ("The Herdr app window is closed. Open it on the computer, then try again."); other errors = refused, with the app's message. There is no retry. Afterwards, one complete core read.
- New space shows the new space in the app window. New terminal focuses the new pane when its tab is shown there.
```

Section 7 (Risks), additions:

```diff
+- **App command interface:** it is used only through the documented `herdr-app` CLI. Exit codes don't separate "not sent" from "unknown", so Shepherdr reads the CLI's stderr and error text, validated per app version.
+- **App actions change the window:** New space switches the shown space, and New terminal focuses the new pane when its tab is shown.
+- **A closed window leaves a stale app layout** after Close terminal or Close space. Home is right through rules 1 and 4, and the app tidies up on its next start.
```

Section 9, "Wave 3", replaced by sections 7.1 and 7.2 of this plan.

### 5.2 `docs/ui-direction.md`, "With the Herdr app"

```diff
-Home lists the app's spaces and tabs in the app's order and with the app's names. Terminals that no space contains appear once in a last group, **Other terminals**. The app has no worktree groups, so none appear, and there are no space or terminal actions. Terminal offers …
+Home lists the app's spaces and tabs in the app's order and with the app's names. Terminals that no space contains appear once in a last group, **Other terminals**. The app has no worktree groups, so none appear. A space can be closed and a terminal closed at any time. **New space**, **New terminal** and **Rename space** appear only while the Herdr app window is open, because the app does them. Their sheets and confirmations use the workspace copy with **space** in place of workspace. There is no **New worktree**, **Delete checkout** or **Rename terminal**. Terminal offers …
```

The 3a-only variant drops the "**New space**, **New terminal** and **Rename space**" sentence.

### 5.3 `README.md`, "With the Herdr app"

```diff
-… **Add files** shows while an agent reports a status. There are no space or terminal actions. herdr-core 0.1.15 is validated; …
+… **Add files** shows while an agent reports a status. You can close a space or a terminal from Home at any time. With the Herdr app window open, you can also make a space, split a terminal and rename a space; these change what the app window shows. There are no worktree actions and no terminal rename. herdr-core 0.1.15 and the Herdr app 0.1.10 are validated; …
```

CHANGELOG: one entry per wave, added before merge as usual.

---

## 6. What changes in TUI mode

- Nothing visible.
- Request bodies gain `herdr: "tui"`. `interruption_counts` gains `error: 0`. The terminal-created route stops hard-coding `"tui"`.
- Existing TUI Go and web tests are updated for the field, and the real TUI action workflows are re-run.

---

## 7. Waves (one worker each; independent review; integrator; AGENTS.md states)

### 7.1 Wave 3a: Close terminal and Close space (core API only)

- **Base:** the approved integrated commit after wave 2 (`06c40b1` or later).
- **Scope:** sections 4.1 (core rows), 4.2, 4.4 (Close copy), 4.5, and the 3a variant of the doc diffs.
- **Likely files:**
  - `internal/herdr/core_client.go`: `Kill(ctx, address)` with exact error kinds; `terminal.kill` in the required methods;
  - `internal/herdr/core_project.go`: core `actions` for document spaces and their terminals, plus Other terminals rows per D6; a small helper for the space's terminals and the workers that end with each;
  - new `internal/server/core_actions.go`: prepare and run for `close_terminal` and `close_workspace` on core, reusing the facts types, `withCommitAuthority`, the refusal reasons and `finishMutation`;
  - `internal/server/server.go`: route by `herdr`; a core coordinator built when core is followed;
  - `internal/server/workspace_actions.go`, `terminal_actions.go`: the `herdr` key; `error` in the counts;
  - `main.go`: wiring;
  - `web/src/workspace-actions.ts`, `terminal-actions.ts`: the `herdr` field and the `error` count;
  - `web/src/home-view.ts`: side-dependent words in menus, sheets, results and refusals;
  - `web/src/main.ts`: the side for created terminals;
  - focused tests built from the recorded JSON; a `web/dist` rebuild; the docs.
- **Validation:**
  - Isolated (`env -i`, all of `XDG_CONFIG_HOME`, `XDG_RUNTIME_DIR`, `XDG_STATE_HOME`, `HC_SESSION`, `HERDR_APP_HOME` and `HERDR_RELEASES_URL` under one `/tmp` folder): herdr-core 0.1.15, Herdr app 0.1.10, and the Herdr TUI 0.9.5.
  - Production build; headless Chromium at a phone viewport (memory-capped entry); then the human's real phone.
- **Acceptance:**
  1. **Close terminal, window open:**
     - one terminal of a split: the app removes the pane;
     - a tab's last terminal: the confirmation says the tab closes, and the app removes the tab;
     - a space's last terminal: the "Close {space}?" confirmation, and the app removes the space.

     In each case Home updates once, without blinking or moving the page.
  2. **The same three with the window closed:** Home is right at once. Reopening the app shows a layout that matches Home.
  3. **Close space:**
     - setup: two tabs, a split, a worker started from an app terminal, and Claude Code working and blocked;
     - the confirmation counts the agents and shows the warning (working, blocked);
     - all terminals end and the space leaves Home and the app;
     - a worker started with `outlive_parent` is killed too.
  4. A Claude Code agent reporting `error` (or a unit test from the recorded event) appears in the warning.
  5. **Stale and races:**
     - the person closes the terminal or space in the app between prepare and confirm → "This terminal is no longer here." (Close terminal), "This space is no longer here." (Close space) or "This space changed…";
     - the person adds a tab during Close space → unknown, then the true Home.
  6. A kept terminal closed by Shepherdr does not restart.
  7. Stopping core during an action gives unknown (or "Herdr is not available" if nothing was sent), with no retry and a fresh Home after reconnect.
  8. **Other terminals** has no space menu. Its rows follow D6.
  9. Hostile space, tab and terminal names render as inert text in menus and confirmations.
  10. **TUI unchanged:** all existing Go and web tests pass, and the real TUI workflow for New space, New worktree, Close workspace, Delete checkout, Split, Rename and Close terminal still works, with core also running.
  11. Isolation record: the human's `terminals.json` hash before and after; additions to `~/.claude.json` noted.
- **Gate for 3b:** independent review approves 3a, and the integrator's run of checks 1–3, 5 and 10 passes on the integrated commit, and the human accepts it on a real phone.

### 7.2 Wave 3b: app window actions (only if D2 is approved)

- **Base:** the approved integrated 3a commit.
- **Scope:** sections 4.1 (app rows), 4.3, the rest of 4.4, and the 3b doc diffs.
- **Likely files:**
  - new `internal/herdr/app_client.go`: find, gate, `status`, `do` with JSON args, and classification; unit tests use a tiny fake `herdr-app` **script** for exit codes only (no large stand-in);
  - `internal/herdr/core_projector.go`: the 5 s window check; publish on change;
  - `internal/herdr/core_project.go`: keep each placed terminal's document block id server-side; app actions when the window is open;
  - `internal/server/core_actions.go`: `create_space`, `split_terminal` (waiting on the published Home), `rename_space`;
  - `main.go`: `herdr-app` lookup and the default-socket pairing rule;
  - `web/src/home-view.ts`: New space in core mode (suggestions shared with the Herdr TUI), the Rename space sheet, the `app_closed` refusal;
  - `web/src/workspace-actions.ts`: `rename_space`;
  - tests; a `web/dist` rebuild; the docs.
- **Acceptance** (same isolation; the isolated app window opens on the desktop and is closed afterwards):
  1. Window open → New space, New terminal and Rename space appear. `do window.close` → they disappear within about 5 s with no blink. Reopen → they return. The app not on `PATH`, a missing command in `commands`, or a custom core socket → none appear, with one log line.
  2. **New space:**
     - a named space at `~/…` and at an absolute folder: the app shows it with that name, and Home lists it;
     - a missing folder → "Action not completed" with the app's message;
     - blank label → an unnamed space named after its folder.
  3. **New terminal:**
     - Beside and Below on a split terminal: the app places it next to that exact pane, and Shepherdr opens the new terminal;
     - a worker row offers no New terminal.
  4. **Rename space:** the app and Home both show the new name. A blank name returns to the automatic name.
  5. **Races:**
     - the person closes the space in the app before Shepherdr's command → refused (`no space`);
     - the window closes between the check and the command → "The Herdr app window is closed…";
     - the reply is withheld (a busy window, if it can be reproduced; else a classification unit test) → unknown.
  6. The app's window switches to the new space and focuses a split's pane, as documented. Nothing else changes in the window.
  7. 3a acceptance and TUI acceptance still pass.

### 7.3 Not planned

Worktree actions (D1), Rename terminal, tab actions, remote machines.

---

## 8. Risks

- **The app's command interface is 0.1.x and changes quickly**, like core. The gate checks arguments, but error text and exit codes are checked only per validated version.
- **CLI exit codes are ambiguous** (exit 2 covers both "not sent" and "hung up after sending"). Shepherdr must read stderr and the error text. The socket would remove the ambiguity, but it is undocumented (D2).
- **App actions change the person's window** (B4, R8). Someone typing in the app when a split lands keeps typing into the new pane.
- **Closed window:** Close actions leave the app's layout stale until the app starts again. Home is correct meanwhile.
- **Close space is several kills.** A failure part-way through is unknown and leaves some terminals running. Home shows the truth after the refresh.
- **Pairing:** the app at its default home is assumed to drive the default core. A custom core socket turns app actions off. Someone with a custom `HERDR_APP_HOME` and the default core gets no app actions, because the app client looks in the default home.
- **Window check every 5 s:** an app action can show for up to about 7 s after the window closes or stops answering. The run then refuses honestly ("not sent").
- **Workers:** core kill spares `outlive_parent` children (they move to **Other terminals**), while the app's own kill ends them. The Close space plan kills them explicitly. Close terminal says which will also close.
- **Kill of a finished terminal looks like success** in core's reply. Shepherdr relies on its fresh read before the kill.

---

## 9. Human decisions (approved 2026-10-31)

1. **D1, worktrees in core mode** (New worktree, Delete checkout). There is no app command; only Git through core `exec` would work.
   - (a) **Keep them hidden. Approved.**
   - (b) Shepherdr runs `git worktree add/remove` through core `exec`, copying the app's mod. This crosses the north-star "no second Git workflow" rule, and the mod's remove forces dirty checkouts.
   - (c) Ask upstream for app commands (for example `worktree.new` and `worktree.remove`) and add them once documented.

   Waiting on it: nothing in 3a or 3b.
2. **D2, use the app's command interface** for New space, New terminal and Rename space.
   - (a) **Yes, through the documented `herdr-app` CLI. Approved.**
   - (b) Yes, through `herdr-app.sock` directly: exact sent/unknown semantics, but undocumented.
   - (c) No: core-only actions (Close terminal and Close space only). A core "New space" would be unnamed and sit in **Other terminals** while the window is closed, so it is not offered.

   Waiting on it: wave 3b.
3. **D3, accept that app actions change the app window** (New space shows it; New terminal focuses the new pane when its tab is shown). The app offers no way to avoid this. **Approved:** accept it and say so in the README. Waiting on it: wave 3b.
4. **D4, Rename space as the core-mode equivalent of Rename terminal.** **Approved** (window open only, TUI rename copy with space words). Rename terminal stays hidden either way. Waiting on it: wave 3b scope.
5. **D5, the Close space route.**
   - (a) **Core: kill each terminal. Approved.** Available with the window closed, which matters most when the person is away from the desk.
   - (b) The app's `space.close`: one call, but only while the window is open.

   Waiting on it: wave 3a.
6. **D6, Close terminal on rows under Other terminals.** Decision A says the group "has no actions". **Approved: allow Close terminal on its rows** (they are real terminals, and kill works), with no group menu. The alternative is no actions there at all. Waiting on it: wave 3a.
7. **New copy (approved):**
   - "N terminals it started will also close.";
   - "The Herdr app window is closed. Open it on the computer, then try again." (3b);
   - "Add a space for an existing folder.";
   - **error** in the interruption warning.

