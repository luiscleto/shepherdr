# Herdr core plan

Status: **approved human direction and work brief** (2026-10-31). The human approved option A (keep the Herdr TUI backend unchanged and add a herdr-core backend), decisions 1–10 below, decisions A–C in section 8 as recommended, and the documentation changes in section 5. Later on 2026-10-31 the human replaced decision 1 and the section 2 flag rules: one Shepherdr follows both, and Home switches between them (section 2, "Both at once"). Waves 1, 1b and 2 are approved for dispatch. Wave 3 (actions) is planned in two parts, 3a and 3b, in `docs/herdr-core-actions-plan.md`; both are approved for dispatch.

Goal (human, 2026-10-31): use the Herdr desktop app instead of the Herdr TUI, and keep the same organization and agent visibility and interaction in Shepherdr that the TUI version has. Release only when core mode supports everything core and the app offer as equivalents of the TUI actions. Later direction: Shepherdr and the desktop app reflect each other's state both ways, so nothing works only on the phone or only on the desktop. The human approves user-facing behaviour, copy and anything security-related; technical choices toward this goal are made by the orchestrator and recorded here.

In this document, **core mode** means what Shepherdr shows for Herdr Core (its Home, terminals and notices), and **TUI mode** the same for the Herdr TUI. Both run at once in one Shepherdr.

Planning base: Shepherdr `628eccb` (v0.4.3). Upstream: herdr-core 0.1.15 (`4a3591a`), Herdr app 0.1.10 (`e3cdccc`).
Date: 2026-10-31. Discovery evidence was recorded outside the repository during planning.

Approved decisions 1–10: (1) keep the TUI backend and add a core backend; one Shepherdr follows both on their sockets whenever each runs, and Home switches between **Herdr TUI** and **Herdr Core**, remembered per browser; (2) read the `herdr-app` document read-only plus terminal labels, never write it; (3) say **space** and **tab** in core mode; (4) Control = active attach, Release = observe, no Take over in core mode, no extra note; (5) hide worktrees, Split and Rename in core mode; (6) show `error` unchanged and treat it as attention like blocked; (7) an agent is a terminal with a current non-null status, identity from `status.app`; (8) hide Send keys combinations core cannot send, uppercase as `shift+<lower>`; (9) gate on API generation 1 and required methods, validated from 0.1.15, newer best effort; (10) this machine's terminals only.
Actions in core mode come in wave 3 (section 3.5 and section 9).

---

## 1. What the real app data showed (Task 1)

The cases were built on the human's real core and app through documented commands only (`herdr-app do space.new | pane.split | tab.new | space.rename | space.close | window.close`, `hc send/keys/run/kill/rm`).

Fixture: Space **alpha** (named, git repo, tab **main** split shell | Claude Code, unnamed tab 2), an unnamed space in `repo/web`, an unnamed space in `~`, a worker started from alpha's shell, and a terminal started outside the app.

### Document shape (`doc.get {name:"herdr-app"}`)

```json
{"value": {"spaces": [
   {"id": "sp…", "name": "alpha", "folder": "/…/repo",
    "tabs": [
      {"id": "tb…", "name": "main",
       "root": {"id": "tb…", "type": "row", "children": [
          {"id": "p…", "type": "block", "content": {"terminal": "t1"}, "size": {"weight": 0.5}},
          {"id": "p…", "type": "block", "content": {"terminal": "t2"}, "size": {"weight": 0.5}}]}},
      {"id": "tb…", "name": "", "root": {"type": "row", "children": [{"type": "block", "content": {"terminal": "t3"}}]}}]},
   {"id": "sp…", "name": "", "folder": "/…/repo/web", "tabs": [ … t4 … ]},
   {"id": "sp…", "name": "", "folder": "/home/luisc", "tabs": [ … t5 … ]}]},
 "version": 4}
```

- **No version or schema field inside the document.** The only version is `doc.get`'s write counter. It went 0 → 1 … 7 over the session and is not a format version.
- A never-written document is `{"value": null, "version": 0}`. That was the human's real state at the start. After every space closed it is `{"value": {"spaces": []}, "version": 7}`.
- Splits are a tree. Node `type` is `row`, `column`, `stack` or `block`, and the app source also has other block contents (`mod`, `tab`, `agent`). Terminals are `block` nodes with `content.terminal`. Depth-first order is the visual order.
- Space and tab ids are opaque (`sp…`, `tb…`). Pane ids (`p…`) are the app's own.
- An unnamed space or tab is stored as `name: ""`. Local spaces have no `machine` key.

### Names

| Case | Document | App shows (`herdr-app ls`) | `where` label |
|---|---|---|---|
| Named space, named tab | `alpha` / `main` | `alpha` / `main` | `alpha / main` |
| Named space, unnamed second tab | `alpha` / `""` | `alpha` / `2` | `alpha / 2` |
| Unnamed space in `repo/web` | `""` / `""` | `repo` / `1` | `web / 1`, then `repo / 1` |
| Unnamed space in `~` | `""` / `""` | `~` / `1` | `~ / 1` |
| After `space.rename … beta` | `beta` | `beta` | `beta / 1` (labels event) |

- An unnamed tab is named by its position: 1, 2, …
- An unnamed space is named after its first terminal's **git top folder**, which the app finds by running git. If it can't, the app uses that terminal's folder, then the space folder. It uses the basename, and `~` for home.
- Without git, Shepherdr can rebuild the exact name only from the `where` label. On every app start the label first says the folder (`web / 1`) and switches to the git top (`repo / 1`) a moment later.

### `where`, `herdr.space`, `herdr.tab` labels

- The app writes them on every terminal it shows, again on every start, and again on rename.
- **They stay after the window closes.** The discovery report's "written only while the window runs" is true for writes. The values persist and go stale only if structure changes while the app is closed, which nothing does except terminals ending.
- On window close every terminal's size went back to headless 200×120 (`size` events).

### Workers and other terminals

- A worker started from inside an app terminal (`hc run --bg` from alpha's shell; an agent's `hc run` looks the same) has `parent: "t1"` and `die_with_parent: true`. It has no `herdr.space` or `herdr.tab`; the app copies the parent's `where` onto it. The app lists it as a **sub-agent of its parent** and gives it no pane.
- A terminal started outside the app has no labels and no parent, and the app doesn't show it at all.

### Identity and `restarted`

- **The app turns `keep` on for every terminal it opens.** So `lost` → `restarted` is the normal path for app terminals, not an edge case.
- Killing alpha tab 2's shell (`kill -9`) gave `lost {reason: signaled, signal: 9}` then `restarted`. Afterwards the address (`t3`), `created_ms`, labels and app pane were all the same; only the pid changed. The app kept it in the same pane.

### Status

- Claude Code on its folder-trust prompt: `status: null, reported: false`. After the trust was accepted: `{"app": "claude-code", "state": "idle"}`. This matches the report. A shell has `null`.

### Events

- Setup produced `created`, `labels`, `doc` (one per structure write), `status`, `size`, `title` and `foreground`. Rename produced `labels` and `doc`. Restart produced `lost` and `restarted`. Window close produced only `size`. There were heartbeats every 15 s.
- A `doc` event carries only `{name, version}`, so a complete read is needed after it.

### Mismatches with the discovery report

1. `where` labels persist after the window closes. The report implied they could be missing.
2. App terminals are all kept, so `restarted` is common (report: an edge case).
3. Workers inherit `where` without `herdr.space`, and the app nests them under their parent.
4. The document has no format version (the report did not say).

Everything else matched: hello (generation 1, 0.1.15, 32 methods, features `framed attach events status shell`), the status words and null before the first report, the label names, and the document shape from `mirror.rs`.


---

## 2. Start flags and connection

### Flags

- There is no mode flag. Shepherdr follows the Herdr TUI (`-herdr-socket`, default unchanged) and herdr-core (`-herdr-core-socket`) at once, each whenever it is running.
- `-herdr-core-socket PATH`: the default is `$XDG_RUNTIME_DIR/herdr-core/default.sock`. If `XDG_RUNTIME_DIR` is unset and the flag is not given, Shepherdr does not follow core: it logs this once at start and shows the Herdr TUI only.
- A relative path in either socket flag is a start error.
- `herdr` on `PATH` is needed only to open Herdr TUI terminals. Without it Shepherdr starts, logs a warning, and Herdr TUI terminals show **Terminal unavailable**.
- Shepherdr never runs `hc connect` or `hc` itself, because those can start a core. It dials the socket only, so a stopped core shows honestly as "not running".
- The ready log line names both sockets.

### Both at once

- Each Herdr has its own connection loop, state, Home, notification evaluator and upload records. Neither waits for or affects the other.
- The Home message carries both complete states, `{epoch, tui, core}`, and is sent whenever either changes.
- A Herdr is *present* once Shepherdr has had a Home from it since starting, or while it answers but can't be used. When both are present, Home shows a switch, **Herdr TUI** | **Herdr Core**. With one present, Home shows it without a switch. Each browser remembers its choice in local storage, and works for the visit without storage. A stopped Herdr stays in the switch with its last-known Home and **Herdr is not running** until Shepherdr restarts.
- Every terminal link names its Herdr: `#terminal=<pane>&terminal_id=<id>&herdr=tui|core`. A link without `herdr` is a Herdr TUI link.
- Notifications come from both, whichever one Home shows. Settings are one set for both.

### Connection states (existing badge words, unchanged)

Each Herdr has its own badge state; Home shows the state of the Herdr it is showing. **Offline** applies to both.

| Situation | Badge | Detail (logged; shown where detail is shown today) |
|---|---|---|
| Socket missing or refused | **Herdr is not running** | as today |
| hello generation ≠ 1, or a required method or feature missing | **Cannot use this Herdr** | "This Herdr Core can't serve Shepherdr: it doesn't offer <method>." |
| Core version < 0.1.15 | **Cannot use this Herdr** | "Herdr Core <v> is older than the supported range; Shepherdr supports Herdr Core 0.1.15 through 0.1.15." |
| Core version > 0.1.15 | **Live** + the existing best-effort warning in the log | as for the TUI |
| `herdr-app` document present but not the expected shape (section 3.1) | **Cannot use this Herdr** | "The Herdr app's layout could not be read." |
| Live, then the socket drops | **Reconnecting**, then **Offline** after 45 s | unchanged loop |

Liveness: core's 15 s `heartbeat` on the event connection counts as liveness, like the TUI's.

### Version gate and required methods

- `hello {generation: 1}` must return `generation: 1`.
- Features must include `events` (wave 1), plus `framed` and `attach` (wave 2).
- Methods required in each wave, which must all appear in `hello.methods`:
  - wave 1: `hello`, `events.subscribe`, `terminal.list`, `doc.get`;
  - wave 2 adds `terminal.attach` and `terminal.keys`;
  - wave 3a adds `terminal.kill`;
  - wave 3c adds `terminal.run`.
- `terminal.run` is used only for two labelled runs while the Herdr app window is closed: **New space** and **New terminal** (wave 3c, `docs/herdr-core-closed-create-plan.md`). Each is `{cwd, labels: {herdr.space, herdr.tab}, keep: true}`, with no `argv`, `name`, `env` or caller. Stale-id rule: a labelled run for a space or tab the app has closed brings it back, so New terminal takes its ids from a fresh complete read just before the run (while the window is closed only the app writes the document, so it can't change), and New space uses new ids that aren't in that read.
- Never used: `terminal.read` (it scrolls full-screen apps), `terminal.send`, `doc.set`, `terminal.put`, `machine.*`, `server.*`, `exec`/`file.*`, and `terminal.run` for anything else.
- Validated range: 0.1.15 only. Newer runs best effort with a warning. Add each version to the range only after the real acceptance below.

---

## 3. Home on core

### 3.1 The loop (same shape as the TUI loop)

1. Dial, then `hello` and the gate.
2. Open one plain `events.subscribe {}` connection. Its reply snapshot is not used for Home.
3. Do one complete read on a short-lived connection: `terminal.list {}` (live terminals only) plus `doc.get {name:"herdr-app"}`. Build and validate a whole Home, then publish it.
4. These events trigger the next complete read: `created`, `lost`, `restarted`, `finished`, `removed`, `status`, `labels`, `title` and `doc`, plus `missed`. Ignore `size`, `foreground`, `turn` and `keep`. Keep one read at a time and one pending refresh, as today. That latch absorbs the noisy `title` events.
5. On a disconnect: reconnect, subscribe, then do a complete read. There is no replay.

How to read the document:
- `value: null`, or no document, means no spaces, which is normal when the app has never run.
- Otherwise `value` must be an object whose `spaces` is an array of objects, each with a string `id` and `tabs`. If not, show **Cannot use this Herdr**.
- Ignore unknown keys and unknown node types. A terminal the document doesn't place still appears (3.3), so tolerant parsing never hides a terminal.

### 3.2 Projection onto the existing Home contract

The core read becomes the existing internal `Snapshot` / `Home` contract, so Home, the filter, Blocked, notifications and uploads work the same way for each Herdr.

| Home field | Core source |
|---|---|
| Workspace (shown as a **space**) `id` | document space `id` |
| Workspace `label` | space `name` if not empty. Otherwise the part before the first `" / "` in the `where` label of the space's first live terminal (in tree order). Otherwise the basename of the space `folder` (`~` for the home folder). A basename can't contain `/`, so the split is safe for unnamed spaces. |
| Workspace `number`, order | document order, starting at 1 |
| Tab `id`, order | document tab `id` and order |
| Tab `label` | tab `name` if not empty, else its position (`"1"`, `"2"`, …), matching the app |
| Tab `current` | always false (focus is app-private) |
| Terminal order in a tab | depth-first over `root`, `block` nodes with `content.terminal` |
| Terminal `pane_id` | core address, e.g. `t7` |
| Terminal `terminal_id` | `<core.id>:<address>` (core.id from `hello`) |
| Terminal `agent` | present only while `status` is non-null: `{kind: status.app, name: status.app, status: status.state}`. The name is `status.app` exactly as reported ("claude-code", "codex", …), never the live title, so it stays stable while the agent works. |
| Terminal `title` | the agent name, else the core `name`, else the terminal `title`, else **Terminal**. Today's single-terminal and duplicate-numbering rules still apply. |
| Terminal `manual_name` | never set (core has no rename) |
| Terminal and space `actions` | as section 3.5 |
| Worktrees, `checkout_path` | never set (hidden in core mode) |

Status words: `idle`, `working`, `done`, `blocked` and `error` pass through unchanged. `unknown` never occurs in core. Any other word fails the read, as the TUI does today (`InvalidStatusError`).

### 3.3 Exactly once: placement rules

Only live terminals (`terminal.list` without `all`) and only this machine's (an address with no `/`) are shown.

1. A terminal placed by the document goes in that place. A document block whose terminal isn't live is skipped. The app drops such panes on its next start.
1b. A live terminal the document doesn't place, with non-empty `herdr.space` **and** `herdr.tab` labels, is shown where the app will put it when the window opens (wave 3c):
   - the space and tab are in the document → in that tab, after the tab's placed terminals, in `created_ms` order;
   - the space is in the document, the tab isn't → a tab with that id at the end of that space;
   - the space isn't in the document → a space with that id after the document's spaces, in order of its first terminal's `created_ms`, one tab per tab id. It is unnamed, so the existing label rule applies: its first terminal's `where`, else the base name of that terminal's starting folder;
   - otherwise (the tab id belongs to another space, or the space id is `other`) → unchanged, under **Other terminals**.

   Such a terminal counts as placed for rule 2, so its workers follow it. A waiting space offers **Close space** but not **Rename space**, and its terminals **Close terminal** but not **New terminal**, because they have no pane yet. The app keeps these labels when it adopts a terminal and rule 1 always wins, so each complete read shows every terminal once, and a terminal moves from rule 1b to rule 1 in one Home update.
2. A live terminal not placed by the document whose `parent` is placed (directly or through other parents) goes in its top parent's tab, right after that parent, ordered by `created_ms`. This matches how the app nests sub-agents.
3. Every other live terminal goes in one last group, **Other terminals**, in `created_ms` order. Its fixed id `other` can't clash with the app's `sp…` ids. The group is listed only when it has terminals. It has no actions and never triggers space notices. (Decision A.)
4. A tab with no live terminals is left out, and so is a space with no tabs left.
5. A terminal placed twice in the document fails the read as incoherent, as today.

### 3.4 Counts, attention, notifications

- Agent totals get an `error` field. The display order is **working, blocked, error, idle, done, unknown**.
- Attention:
  - the working count is unchanged;
  - **N blocked** is unchanged and opens **Blocked**;
  - in core mode, a nonzero **N error** appears after it and opens a temporary **Error** view, built the same way as **Blocked**, with the same **Show all terminals** return.
- Status notifications: add an `error` choice, on by default like `blocked`. The notice is "A space needs attention." with the status word, as blocked has. Decision B: on by default, and workers notify like any agent.
- Workspace notices become space notices in core mode: opened or closed is worked out from document spaces, with the existing diff and the existing settings keys. The **Other terminals** group never triggers them.
- `restarted`: the identity (`core.id:address`) stays the same, so Home keeps the same row and notifications don't treat it as a new terminal. The agent status usually goes back to null, so the agent badge disappears.

### 3.5 Actions in core mode

Shown, through core, whenever core Home is live (the app window open or closed):
- **Close terminal**: `terminal.kill` on the exact address. The confirmation is the TUI's, plus "N terminals it started will also close." when workers end with it. If nothing else Home shows would remain in the space, the confirmation is Close space's, and confirming closes everything shown in the space, as the TUI does for a last pane.
- **Close space**: `terminal.kill` on every live terminal Home shows in the space, workers included: workers first, deepest first, each before the terminal that started it, otherwise in Home order. The confirmation is the TUI's Close workspace one in space words, never naming linked workspaces, with `error` in the interruption warning. **Other terminals** has no space actions.

Shown with the Herdr app (wave 3b; `herdr-app do`, see section 4b), while the window is open:
- **New space**: `space.new` with the folder (`~` expanded by Shepherdr) and the optional label as its name;
- **New terminal**: `pane.split` beside or below an exact terminal that has a pane;
- **Rename space**: `space.rename`.

While the window is closed (wave 3c, `docs/herdr-core-closed-create-plan.md`), with a warning on the sheet:
- **New space**: the folder only (`~` expanded, the folder checked), then `terminal.run` labelled with a new space and tab id. The app adds it, unnamed, at the end of its list when the window next opens;
- **New terminal**: one **New terminal** button (no Beside or Below) on a terminal the document places in a pane. Its current folder (`foreground.cwd`, else `cwd`) is checked, then `terminal.run` is labelled with its space and tab. The app adds it as that tab's last pane. The new terminal opens, as after a split;
- **Rename space** stays hidden.

Home carries `app_window: "closed"` while the window is closed, and the browser sends `best_effort: true` with what the person confirmed on that sheet. The server routes by that flag, never by a fresh window check: a confirmed open-window sheet sent after the window closed gets "The Herdr app window is closed. Open the action again to start it in Herdr Core." Without the app (no `herdr-app`, or a custom core socket) none of these show.

Hidden in core mode:
- worktree groups, **New worktree** and **Delete checkout** (core and the app expose no worktree command);
- **Rename terminal** (core names are fixed at start, and the app has no terminal rename);
- **Take over** and **Take over and send**;
- every three-dot menu with no actions.

Expand all and Collapse all only apply to worktree groups, so they never show.

---

## 4. Terminal page on core (wave 2)

The browser keeps its WebSocket protocol, and the server holds the core channel. The bridge is a Go framed client in-process, so there is no `herdr` subprocess.

| Browser need | Core |
|---|---|
| Open, mode `control` | `terminal.attach {terminal, cols, rows, observe:false, history:0, display}` with the browser's cols and rows, as a Herdr TUI terminal opens. `display` holds the browser terminal's colours, so core answers a program's colour queries with them while Shepherdr controls. Core treats the attach itself as activity, so the terminal takes this size until someone types in the app window. It is never "occupied". (Decided by the human 2026-10-31; before, it opened observing.) |
| **Control** (mode `control`), after **Release** | Close the observe channel, then `terminal.attach {observe:false, display}` with the browser's cols and rows. This size applies, as when opening. |
| **Release** | Close the active channel, then reattach with `observe:true`. Observing never resizes, and observe attaches send no `display` (core 0.1.15 never answers from an observer's colours). |
| mode `takeover` | Rejected (400) in core mode. The browser never sends it, because ownership is never "occupied". |
| Reader older output (today's `pane read --source recent --lines N`) | A short separate observe attach with `history: N`. Read DATA up to and including the first `CSI ?9001h` or `CSI ?9001l`, which ends the first paint, then CLOSE. Wait no longer than the existing request timeout; on time-out Reader shows its existing "Could not read terminal output". Reader gets the paint without its title and status strings, its character set choices, its clears, and the blank lines that push history into scrollback. The line break that ends the last history line stays, so each row is one Reader line. Never `terminal.read`. (Amended 2026-10-31, see the note below.) |
| Live output | The first DATA chunk → one `terminal.frame` with `full:true`; every later DATA chunk → `full:false`. Width and height are the terminal's current size, from the attach result and `size` events. (Amended 2026-10-31.) |
| Flow control | Send CREDIT for the bytes forwarded to the WebSocket. A fresh repaint after 4 MiB of backlog arrives inside the byte stream as a delta; it resets the screen itself. (Amended 2026-10-31.) |
| **Message** text and Enter | The existing chunks (bracketed paste, then `\r`), written as DATA on the active channel, in order and under the existing write lock. **Accepted** means the bytes were written to the core socket. A failed or partial write is **unknown**. There is no retry. |
| **Send keys** | `terminal.keys {terminal, keys:[name]}`, only while controlling. Core waits up to ~3 s after the last typing ("quiet rule"); note this as a known limitation. |
| Resize while controlling | A RESIZE frame (u16 cols, u16 rows) on the active channel. Never while observing. |
| Files (Reader Send, full-terminal Insert) | Unchanged local staging, with core records kept in their own file (`uploads-core.json`), apart from the Herdr TUI's. **Other terminals** keeps its files while the group is listed. Paths are sent as Message chunks. **Add files** shows only while the terminal has a non-null status. `terminal.put` is not used. |
| Program ends, or `lost` | Core CLOSE `{reason, …}` → a `terminal.status` with that reason, then the stream ends. The page's existing reconnect then attaches to the restarted shell once core has it: a phone page retries about every second in its last mode, and a desktop page reconnects observing. (Decided 2026-10-31.) |
| Exact target | Valid while Home is live and not last-known, and the latest Home still contains `pane=address` with the same `terminal_id=core.id:address`. There is no layout-rectangle check in core mode. |

The phone and the app window can both type. Size follows whichever typed last. There is no note about this (decision 4).

**First paint amendment (approved 2026-10-31).** Core 0.1.15 does not send the first paint as one DATA frame. It sends stream bytes in chunks of up to 16 KiB, and live output can share the paint's last chunk. The attach result carries no paint length, and neither the published API nor core's own `hc attach` and `hc read` mark where the paint ends. In 0.1.15 every paint ends with `CSI ?9001h` or `CSI ?9001l`, followed only by status reports, and every paint resets the screen itself. So Reader cuts the paint at the first such marker, and the live stream marks only its first chunk as full. Reader's renderer can't show a raw paint, so Reader gets it cleaned as the Reader row says. This relies on core 0.1.15 behaviour and is validated per version, like the rest of the core range.

### Send keys mapping (Shepherdr name → core name)

| Shepherdr | Core |
|---|---|
| `enter` / `return`, `esc` / `escape`, `tab`, `backspace` / `bs`, `left` `up` `down` `right`, `space`, `f1`–`f12` | the same names |
| `minus` `comma` `period` `slash` `backslash` `quote` `semicolon` `backtick` | `-` `,` `.` `/` `backslash` `'` `;` `` ` `` |
| One character `a`–`z`, `0`–`9`, `` ` `` `\` `[` `]` `,` `=` `-` `.` `'` `;` `/` | the same character |
| One character `A`–`Z` | `shift+<lower>`, merged with any chosen modifiers (decision 8) |
| Modifiers: Ctrl, Alt / Option, Shift, Super / Command | `ctrl`, `alt`, `shift`, `super`, in that order, joined with `+` |

These are hidden in core mode, because core has no key for them:
- `double_quote`, `colon`, `percent`, `ampersand` and `plus`;
- every other Character-picker character (shifted symbols such as `! @ # $ ^ * ( ) _ { } | < > ? ~ " : % & +`, and anything outside ASCII);
- `hyper`, which is not offered today either.

The Character field only accepts the characters in the table. Any other input gets the inline note "Herdr can't send this key." and Send stays unavailable.

The existing Super note stays: it reaches only programs that turned on enhanced keyboard input. Core refuses a combination it can't encode for the program's current keyboard mode with `invalid_params`; Shepherdr shows "This program can't receive that key combination right now." (see section 5).

---

## 4b. App window actions (wave 3b)

- Shepherdr runs `herdr-app` from `PATH` only as `--version`, `commands`, `status` and `do <command> <one JSON object>`, with no shell. It never runs bare `herdr-app`, which opens the window. If `herdr-app` is missing, or Shepherdr follows a non-default core socket, app actions are off and this is logged once.
- Gate: herdr-app 0.1.10 validated; newer runs best effort with a warning. `commands` must list `space.new {name, folder}`, `pane.split {direction, pane}` and `space.rename {id, name}`.
- Window open = `herdr-app status` exits 0 within 2 s. Home asks every 5 s and after each action, apart from the core reads, so a window that stops answering reads as closed without holding up Home. Each app action also asks first. A change publishes one complete Home.
- Results: exit 0 = succeeded; "the window did not answer", "the window hung up", or a timeout = unknown; "no Herdr window" = not sent ("The Herdr app window is closed. Open it on the computer, then try again."); other errors = refused, with the app's message. There is no retry. Afterwards, one complete core read.
- New space shows the new space in the app window. New terminal focuses the new pane when its tab is shown there.
- Window closed = `herdr-app status` didn't answer. Before the first answer, Home offers no app actions. While closed, New space and New terminal run in core (section 3.5); when the window next opens, the app adopts the labelled terminals within about 1 s without changing what it shows.

---

## 5. User-facing copy in core mode

TUI mode copy is unchanged. In core mode:

| Where | TUI mode (today) | Core mode |
|---|---|---|
| Masthead | **Herdr TUI 0.9.5** | **Herdr Core 0.1.15** (the herdr-core version) |
| Switch (both present) | **Herdr TUI** \| **Herdr Core** | same |
| Notifications settings, both present | Get an alert when a **workspace or space** needs attention or finishes. **Workspace or space opened** / **Workspace or space closed** | same |
| Home filter label | Filter workspaces and terminals | **Filter spaces and terminals** |
| Unplaced terminals group | (n/a) | **Other terminals** (decision A) |
| A lone Other terminal | (n/a) | Shows its own name, with **Other terminals** beneath it; it is not renamed **Other terminals** |
| Space with no name, no `where` label and no folder | (n/a) | **Space** |
| Attention | N blocked | N blocked, and **N error** when nonzero |
| Temporary view | Blocked | Blocked, or **Error** |
| Agent status words | working, blocked, idle, done, unknown | working, blocked, **error**, idle, done |
| Notifications settings intro | Get an alert when a workspace needs attention or finishes. | Get an alert when a **space** needs attention or finishes. |
| Notification choices | Workspace opened / Workspace closed | **Space opened** / **Space closed**; add **error** |
| Notice titles and bodies | Workspace opened/closed; "A workspace needs attention." etc. | **Space opened/closed**; "A **space** needs attention." etc. The status notice for `error` uses the `blocked` wording. |
| Error notice title | (n/a) | **Agent status is error** |
| Status notice body for an agent under Other terminals | (n/a) | "A **terminal** needs attention.", "A **terminal** is working.", "A **terminal** is idle.", "A **terminal** finished."; `error` uses the `blocked` wording |
| Terminal page before wave 2 | (n/a) | **Terminal unavailable** with **Home**, without "This terminal is no longer here." Core mode is not released before wave 2. |
| Terminal control action | Control / Take over / Release | **Control** / **Release** only |
| Observing hint | "Use Control first." / "Use Take over first." | "Use Control first." only |
| Send keys, unsupported character | (n/a) | "Herdr can't send this key." |
| Send keys, Character help line | "Type one character, including a space or +." | "Type one character, including a space." |
| Send keys, a combination core refuses for the program's keyboard mode (`invalid_params` from `terminal.keys`, such as Ctrl + Super + c in an ordinary shell) | (n/a) | "This program can't receive that key combination right now." The combinations stay offered: they work once a program turns on enhanced keyboard input. |
| Send keys, any other key not sent | "Key not sent. Check control and try again." | unchanged |
| Connection badge | Live, Reconnecting, Offline, Herdr is not running, Cannot use this Herdr | unchanged |
| Empty Home | No terminals / Herdr is running, but nothing is open. | unchanged |

The browser gets both Herdrs' states in each Home message (`tui`, `core`) and words Home for the one it shows. Every notification payload carries `"herdr": "tui" | "core"`, so the service worker can pick the wording. A status notice for an agent under Other terminals also carries `"other_terminals": true`, so it can say terminal.

### Approved `docs/ui-direction.md` change (applied in wave 1)

```diff
@@ ## Voice
-Use familiar words: **workspace**, **terminal**, **agent**, **worktree**, **group**, **device**, and **notification**. Keep internal words such as source, pane, route, and session off the screen. **Linked workspace** may appear only when someone needs to understand the additional work an action affects.
+Use familiar words: **workspace**, **terminal**, **agent**, **worktree**, **group**, **device**, and **notification**. When Shepherdr is started for the Herdr app (herdr-core), say **space** and **tab** instead of workspace, matching the app. Keep internal words such as source, pane, route, and session off the screen. **Linked workspace** may appear only when someone needs to understand the additional work an action affects.
 
-Use Herdr's status words unchanged: **working**, **blocked**, **idle**, **done**, and **unknown**. Supporting text may say a blocked agent “needs you,” but that is not another status. Show paths and raw errors only when they help someone act. Do not show internal IDs.
+Use Herdr's status words unchanged: **working**, **blocked**, **idle**, **done**, and **unknown**, plus **error** from the Herdr app. Treat **error** like **blocked** for attention: when nonzero, **N error** follows **N blocked** and opens a temporary **Error** view. Supporting text may say a blocked agent “needs you,” but that is not another status. Show paths and raw errors only when they help someone act. Do not show internal IDs.
@@ ## Home and attention
+### With the Herdr app
+
+Home lists the app's spaces and tabs in the app's order and with the app's names. Terminals that no space contains appear once in a last group, **Other terminals**. The app has no worktree groups, so none appear, and there are no space or terminal actions. Terminal offers **Control** and **Release** but no **Take over**: the phone and the app window can both type, and the size follows whoever typed last. **Send keys** shows only keys Herdr can send.
+
```

The human also approved a matching one-line note in `docs/north-star.md` ("Uses Herdr's words") and `AGENTS.md` ("Use Herdr's status words unchanged") that **error** appears with the Herdr app. Wave 1 applies all three.

---

## 6. What stays untouched in TUI mode

- `-herdr-socket` and its default, the snapshot loop and the 0.9.0–0.9.5 gate. (`herdr` on `PATH` is no longer a start requirement; see section 2.)
- Worktree groups and actions, Split, Rename, Close, New space, Take over and its confirmation.
- The `herdr` subprocess terminal bridge, `pane read` for Reader, and today's key names (`plus`, `percent` and the others stay).
- All current copy ("workspace").
- The only shared code changes are additive:
  - the `error` status and count;
  - the two-Herdr Home message, the switch and `herdr` in terminal links;
  - a backend seam in the projector and terminal bridge.

  The TUI never produces `error`, so its Home and notifications behave the same.

---

## 7. Risks

- **The app document is private to the app** and has no format version. A format change shows **Cannot use this Herdr** (if the shape breaks) or moves terminals into **Other terminals** (if node types change). Either way it's visible, never a silent loss.
- **Core releases daily.** Validate each version before claiming it; newer versions run best effort.
- **`where` flicker on app start:** an unnamed space's name briefly shows its folder, then the git top.
- **First paint end marker:** core does not delimit the first paint, so Reader cuts at the paint's own end marker (see the First paint amendment in section 4). It relies on core 0.1.15's paint layout and must be re-checked for each new core version.
- **No acknowledgement for raw input:** a Message is "accepted" once written to the core socket. The TUI's acknowledgement is not stronger in substance, but the wording must stay "sent to the terminal".
- **Keys can wait up to ~3 s** under core's quiet rule while someone is typing in the app.
- **Shared input:** the phone and the app window can type into the same terminal at once (approved, no note).
- **No status before an agent's first report,** for example during trust prompts, so **Add files** is unavailable then.
- **Links already on screen** when a terminal opens are not clickable, because the first paint drops OSC 8.
- **A closed window leaves a stale app layout** after Close terminal or Close space. Home is right through rules 1 and 4, and the app tidies up on its next start.
- **App command interface:** it is used only through the documented `herdr-app` CLI. Exit codes don't separate "not sent" from "unknown", so Shepherdr reads the CLI's stderr and error text, validated per app version.
- **App actions change the window:** New space switches the shown space, and New terminal focuses the new pane when its tab is shown.
- **Creating while the window is closed predicts placement** (rule 1b). Position, order, unnamed naming and `keep` come from app 0.1.10's adoption code and real runs, and are validated per app version. On reopen, a new space's name can change once (folder → git top), several new spaces, or several new terminals in one tab, can swap places (the app places them in the order its checks finish, which varies from run to run), and a re-created stale tab can move. A labelled run for a space or tab closed in the app between the fresh read and the run brings it back; that window is a sub-second race and is accepted. A refused run uses up a core address, so the folder is checked first. An app at another home on the same core makes "closed" wrong, but the result is still right.
- **Closing with the window open can flash for one frame:** when the app closes a space or pane with the window open, Home can show the closing terminal for about one frame at the end of its space or of the list, as it did under **Other terminals** before.

---

## 8. Decisions (approved as recommended)

**A. Terminals that no space contains.**

Workers started by agents (`hc run`) and terminals started outside the app are real terminals, sometimes agents with status. The north star says every real terminal appears exactly once; the app hides them.

- **(1) Approved:** place a worker in its parent's tab, right after the parent. Show everything else once in a last group, **Other terminals**.
- (2) Hide them, as the app does. This breaks "every real terminal exactly once" and hides blocked workers.
- (3) Put all unplaced terminals, workers included, in **Other terminals**.


**B. Notifications for `error` and for workers.**

- **Approved:** `error` notifications start on, like `blocked`, and workers notify like any agent.
- Alternative: match the app, which doesn't alert for sub-agents. That adds a rule Shepherdr doesn't have today.


**C. A malformed app document.**

- **Approved:** **Cannot use this Herdr** with "The Herdr app's layout could not be read."
- Alternative: show every terminal under **Other terminals** and log a warning. Home keeps working, but the app's structure silently disappears.


---

## 9. Waves (one worker each; review and integration as AGENTS.md)

Each wave starts from the approved integrated commit of the one before. Wave 1 starts from `628eccb` (or the human's chosen integrated commit, if `master` has moved).

### Wave 1: Home on core

- **Scope:** sections 2, 3 and 5 (Home and notification copy); decisions A–C as approved; the ui-direction change if approved.
- **Likely files:**
  - `main.go`, `main_test.go` (flags, wiring, no `herdr` lookup in core mode);
  - new `internal/herdr/core_client.go` (dial, hello gate, subscribe, list, doc.get);
  - new `internal/herdr/core_project.go` (core → `Snapshot`);
  - `internal/herdr/types.go` (`error`, counts, no actions for core);
  - `internal/herdr/projector.go` (a small backend seam: complete read + subscription);
  - `internal/herdr/errors.go` (core compatibility text);
  - `internal/notifications/types.go`, `manager.go`, `sender.go` (`error` choice, mode field);
  - `web/src/home-model.ts`, `home-parser.ts`, `home-view.ts` (words, `error`, Error view);
  - `web/src/notifications.ts`, `web/src/service-worker.js`;
  - focused unit tests built from real core JSON captured during validation;
  - a `web/dist` rebuild.
- **Acceptance gate** (production build, real phone, real app on a disposable core session or the human's core with the human's consent):
  1. With `-herdr-core` the badge is **Live**. A stopped core shows **Herdr is not running**. A core missing a required method shows **Cannot use this Herdr**; a test core is fine here. A newer version logs the warning. Each flag-combination error from section 2 is refused at start.
  2. Home matches `herdr-app ls` for: a named space and tab; an unnamed tab (number); an unnamed space in a git subfolder (git top name once the label settles) and in `~`; a two-way split (both terminals, in tree order); and after a rename in the app (Home updates without blinking).
  3. Each live local terminal appears exactly once, including a worker started from an app terminal and a terminal started outside the app, placed as decision A says.
  4. With the app closed, Home still shows the same spaces, names and terminals.
  5. Claude Code on its trust prompt shows no status, then `idle`, then `working`, `blocked` (a question), then `done`. Counts, **N blocked** and notifications follow.
  6. `error` (if a real agent can be made to report it; otherwise a unit test from the recorded event shape) shows in counts and **N error**, and notifies as decided.
  7. Killing a kept terminal (`restarted`) keeps the same row, and its status clears.
  8. Restart the core of a disposable session; disconnect the network; suspend the phone. Each recovers with a fresh complete read.
  9. Hostile space, tab, title and `where` values render as inert text.
  10. The TUI acceptance passes unchanged in TUI mode.

### Wave 1b: both at once

- **Base:** `0bf96fb`.
- **Scope:** sections 1–8 of `docs/herdr-core-both-plan.md`, except the wave 2 parts (the core bridge, the core uploads manager, and `herdr` on terminal endpoints); rereview finding A; and the doc edits in its section 9.
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
  - the uploads test reworked (`docs/herdr-core-both-plan.md` section 8);
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

### Wave 2: Terminal on core

Wave 2 starts from the approved integrated wave 1b commit. With both Herdrs followed at once, it changes as follows; everything else in this section stands.

- **Added scope:**
  - `/api/terminal`, `/api/terminal/read` and `/api/terminal/files` take a required `herdr` parameter. The server routes to the TUI bridge or the new core bridge, each checking targets against its own projector.
  - Terminal routes are registered when either bridge exists. The core bridge needs no `herdr` binary.
  - The second upload manager (`uploads-core.json`), wired only to the core projector, with the **Other terminals** rule (decision D5 of `docs/herdr-core-both-plan.md`). File-send agent presence for core = a non-null status, as plan 4 already says. `Status.Valid()` currently rejects `error`; the review note stands.
  - Core's `can_open_terminals` becomes true when the core bridge exists, as the TUI's already is with its bridge.
  - README and CHANGELOG drop "not available yet".
- **Added acceptance:**
  - With both running, a TUI terminal and a core terminal are open in two tabs at once, each controlling its own. Notification taps from each side open the right one.
  - Staged files for a TUI workspace and a core space are kept and removed independently: closing the core space removes only core files, and the reverse.
  - Other terminals files stay while the group is listed, survive a Shepherdr restart while it still is, and are removed once it's gone.
  - The existing wave 2 checks apply to core; TUI Terminal acceptance is unchanged.
- **Release:** still not before wave 2 is integrated and the human has accepted both side by side on a real phone.

- **Scope:** section 4 and the terminal copy in section 5.
- **Likely files:**
  - new `internal/server/terminal_core_bridge.go` (framed client: attach, credit, resize, close);
  - `internal/server/terminal_bridge.go` (backend choice);
  - `terminal_target.go` (core target rule);
  - `terminal_keys.go` (core name mapping);
  - `terminal_protocol.go` (no takeover in core, Reader read through attach history);
  - `file_uploads.go` (agent presence = status);
  - `main.go` wiring;
  - `web/src/terminal-page.ts`, `terminal/ownership.ts`, `terminal/keys-sheet.ts`, `terminal/keys.ts`, `terminal/reader-view.ts`;
  - tests and a `web/dist` rebuild.
- **First step:** confirm on a disposable core how the first paint arrives (one DATA frame?). Stop and report if it doesn't.
- **Acceptance gate:**
  1. Reader shows history with colors, then live output. An OSC 8 link printed after opening opens through **Open link?** in full Terminal. Opening controls the terminal, so it takes the opening browser's size until someone types in the app window, and typing in the app is unaffected. (Amended 2026-10-31: core terminals open in control.)
  2. **Control**: Message sends text and Enter into Claude Code. Send keys sends Esc, Ctrl+C, the arrows, F5, Tab, an uppercase letter (as Shift) and a Super combination. Unsupported characters are not offered. The size follows the phone while it types and returns when the app window is used.
  3. **Release** goes back to observing, and the size is no longer the phone's once the app types. No **Take over** appears anywhere.
  4. Files: exact bytes are staged and their paths inserted, for a terminal with status. **Add files** is absent for a shell and for an agent on its trust prompt.
  5. A `restarted` terminal ends the open stream honestly, and the page's existing reconnect then shows the new shell. A closed space makes Terminal unavailable, with no fallback.
  6. Reconnect, suspend, and a core restart recover with no input replay.
  7. TUI Terminal acceptance is unchanged.

### Wave 3a: Close terminal and Close space (core API only)

Planned in `docs/herdr-core-actions-plan.md` (approved 2026-10-31), sections 4.2 and 7.1, with decisions D5 (a) and D6:
- **Close terminal** and **Close space**, both through `terminal.kill`, whenever core Home is live, the app window open or closed. Close terminal also shows on rows under **Other terminals**, which has no space menu.
- Prepare (a fresh complete read, then facts), confirm, then run (a fresh complete read; the facts must still match, else "This space changed…"; then kill). One action at a time per Herdr. Outcomes are succeeded, refused or unknown, with no retry, and one complete read afterwards.
- **Gate:** the acceptance checks in section 7.1 of that plan, on an isolated herdr-core 0.1.15, Herdr app 0.1.10 and Herdr TUI 0.9.5.

### Wave 3b: app window actions

Planned in the same document, sections 4.3 and 7.2, with decisions D2 (a), D3 and D4:
- **New space**, **New terminal** and **Rename space**, through the documented `herdr-app` CLI (section 4b), shown only while the app window answers `herdr-app status`, and only while Shepherdr follows the default core socket.
- **Gate:** the acceptance checks in section 7.2 of that plan, on an isolated herdr-core 0.1.15, Herdr app 0.1.10 and Herdr TUI 0.9.5.

### Wave 3c: create with the app window closed

Planned in `docs/herdr-core-closed-create-plan.md` (approved 2026-10-31), decisions H1–H7: New space and New terminal through a labelled `terminal.run` while the window is closed, and rule 1b in section 3.3.
- **Gate:** the acceptance checks in section 6 of that plan, on an isolated herdr-core 0.1.15, Herdr app 0.1.10 and Herdr TUI 0.9.5.

### Later, separately

Dropping the TUI backend, and remote machines.
