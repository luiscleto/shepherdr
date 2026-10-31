# Herdr Core create actions with the app window closed

Status: **approved human direction and work brief for wave 3c** (2026-10-31). The human approved H1–H7 as recommended, including the copy in section 5. Technical choices in section 3 are the orchestrator's.

Planning base: Shepherdr `03455ea` (master). Upstream: herdr-core 0.1.15 and Herdr app 0.1.10 (the human's installed binaries, copied; app source `e3cdccc` read for the adoption code). Date: 2026-10-31.
Validation evidence was recorded outside the repository during planning.

The human's request: when the app window is unavailable, still offer **New space** and **New terminal** as best effort through core, with a warning. **Rename space** stays app-only.

---

## 0. Summary

- **It works, and the app's own skill documents the route.** "A terminal started with hc opts into the window with `herdr.space=<id>`. `herdr.tab=<id>` chooses its tab" (`herdr-app --skill`). Shepherdr runs one core call: `terminal.run {cwd, labels: {herdr.space, herdr.tab}, keep: true}`. When the window next opens, the app adopts the terminal within about 1 s and doesn't change what the window shows (validated, section 2).
- **New terminal** lands at the end of the target's tab, in the target's current folder. Beside/Below can't be chosen.
- **New space** lands as an **unnamed** space at the end of the list, named after its folder. The typed name can't be carried: core names reject free text, and the app ignores any other label. Recommendation: no Label field while the window is closed (H2).
- **The stale-id hazard is closed by a fresh read.** While the window is closed, only the app writes the document, so it can't change. A fresh read just before the run therefore holds the app's own current ids. A closed space or tab can only come back in a sub-second race in which the window opens and the person closes it right then.
- **Home: one small projection rule** (section 4). A live terminal the document doesn't place, but whose `herdr.space` and `herdr.tab` labels are set, is shown where the app will put it. Rule 1 (the document) always wins, and adoption doesn't change the labels. So there is nothing to reconcile: in each complete read every terminal is shown exactly once, and it moves from the new rule to rule 1 in a single Home update. This was checked on real reads across a reopen (section 4.3).
- **One wave, one worker** (section 6).

---

## 1. Validation setup

- An isolated core (session `cc`, socket `/tmp/shepherdr-closed-create/run/herdr-core/cc.sock`) and an isolated app home (`/tmp/shepherdr-closed-create/apphome`). Every `hc` and `herdr-app` call ran through `bin/iso` (`env -i`, all XDG dirs, `HC_SESSION`, `HERDR_APP_HOME` and `HERDR_RELEASES_URL=http://127.0.0.1:9` under `/tmp/shepherdr-closed-create/`). The window used the Wayland display, given as an absolute path.
- Baseline, built through the app:
  - **alpha** (a git repo; tab **main** split t1 | t2; unnamed tab 2 with t3);
  - an unnamed space in `plain` (t4);
  - **gamma** (t5), then closed with `space.close`, to get stale ids.
- The window was closed with `do window.close`, which quits on Linux; `status` then exits 2.
- Then: creates while closed, a reopen, a stale tab, creates while open, and a full trace of every read across one reopen (`scripts/snap.py`).

---

## 2. What happened (validated)

| # | Case | Result |
|---|---|---|
| C1 | Closed. `terminal.run {cwd: t1's current folder (repo/sub), labels: alpha / main}` | t6 is live, with `argv /bin/bash`, no `where` and `keep:false`. The document stays at **v6**. |
| — | Environment | t6 has the same `TERM`, `COLORTERM` and `TERM_PROGRAM` as the app-made t1, and the same `argv` (core's shell). **No `argv`, `env` or `name` is needed.** No caller means `parent: null`. |
| C3, C3b | Closed. Two runs labelled with a **fresh** space and tab id (`sp`/`tb` + 32 hex, the app's shape), cwd `repo/sub`. C3 also had core `name: "api-work"` and a free label `shepherdr.name: "API work"`. | Both are live and unplaced, in one labelled tab. |
| C5 | Closed. A second fresh space, in `plain` | Live and unplaced. |
| C6 | Closed. Labelled with **gamma's ids** (closed in the app earlier) | Accepted by core. |
| C7 | `name: "API work"` | Refused: `bad name "API work": use 1-32 of a-z 0-9 - _ . starting with a letter or digit, not shaped like an id`. **Core names can't carry a typed space name.** |
| C8, C9 | A missing folder; `cwd: "~"` | `spawn_failed`, "cannot start "/bin/bash": No such file or directory". Each failure used up an address (t12, t13 never appear). **Shepherdr must expand `~` and check the folder first**, which it already does for New space. |
| Reopen 1 | Window opened | Within ~1 s the document went **v6 → v7** (t6 placed, `keep` turned on) **→ v8** (the rest). The window still showed the same space and tab. |
| | C1 → | t6 is the **last pane of alpha / main** (`t1 t2 t6`). |
| | New spaces → | **Appended after the existing spaces, unnamed** (`name: ""`), in this order: C5's space (t10), C3's space (t8, t9), gamma (t11). This run happened to match address-string order (`t10 < t11 < t8`), **not creation order**. That order is not fixed: see the adoption notes below. |
| | Names → | C3's space folder is `repo/sub`, but the app's name, from `where`, is **`repo`** (its git top). |
| | Other labels → | `shepherdr.name` stayed on t8 but was ignored. Core `name` is not a space name. |
| | gamma → | **It came back, unnamed** ("plain"). This repeats R1. |
| E2 | Tab 2 of alpha was closed in the app, then a run labelled with that **stale tab id** while closed, then a reopen | The tab **came back unnamed** (shown as "3"), inserted **after alpha's selected tab** (`add_tab`). |
| E3 | Window **open**. A fresh-space run plus an alpha / main run | Both were placed **~730 ms** after the run (doc v11 → v13). The window's shown space, tab and focused pane **did not change**. Unlike `pane.split` (R8), adoption keeps focus. |
| Split folder | App `pane.split` from t1 (moved into `repo/sub` by `cd`) | t18 started in **`repo/sub`**, so the app uses the terminal's **current** folder. Core's `terminal.list` has it as `foreground.cwd`; `cwd` is only the starting folder. |
| R3 | Closed: a fresh space in `repo/sub` (t19), alpha / main beside t2 (t1b), and a worker of t19 (`parent: t19`). Then a reopen, recording every distinct read. | Five reads: before, after the runs, then v15 (t1b placed; t19's `where` = `repo / 1` written **before** its placement), then v16 (t19 placed). After the reopen, `herdr-app ls` showed the worker as a **sub-agent of t19**. See section 4.3. |

Adoption in the app source (`window.rs` `reconcile` / `start_adoption` / `adopt` / `adoptable`):
- Every live terminal with a non-empty `herdr.space` and no pane is adopted once the document has been read.
- Adoption checks `state == live` and that the labels are unchanged, then turns `keep` on.
- An unknown space is created under the label's id, unnamed, with the terminal's folder.
- A known tab gets a split **right of its last pane**.
- Several waiting terminals start their adoption in address-string order (`orphans.sort()`). Each first waits for its own `terminal.get` on a background thread and is placed when that reply arrives. So several new spaces, or several new terminals in one tab, are placed in the order their replies finish, which varies from run to run. Six real trials in the wave 3c fix matched neither address-string order nor space-id order.
- An unknown tab is inserted after the space's selected tab.
- The shown space and focus are kept.
- `terminal.park` clears both labels, so parked terminals are never adopted.

---

## 3. Design

### 3.1 When the actions show

| Action | Window open (as today) | Window closed (new) |
|---|---|---|
| **New space** (tools row) | `space.new`, named, the window switches to it | best effort through core |
| **New terminal** (menu of a terminal the document places) | `pane.split`, Beside/Below | best effort through core, at the end of that tab |
| **Rename space** | yes | hidden (unchanged) |
| Close terminal, Close space | core | core (unchanged) |

- **Same pairing rule as app actions.** The best-effort actions show only while `herdr-app` passed its gate, Shepherdr follows the default core socket, and `herdr-app status` says closed. Without the app client, Shepherdr can't tell open from closed, and the promise "the app will place it" means nothing, so no create actions show (as today).
- **Contract** (technical; the orchestrator decides):
  - core Home gains `app_window: "closed"` (omitted otherwise);
  - `create_space` and `split_terminal` are offered in both states;
  - the browser picks the sheet from `app_window` and sends `best_effort: true` with what it confirmed;
  - the server routes **by that flag**, not by a fresh window check:
    - `best_effort` → core run, which is correct whether or not the window has since opened (E3);
    - no flag and the window closed → `app_closed`, as today, and Home re-reads.
  - Nobody gets a route they didn't confirm.

### 3.2 New terminal, window closed

- **Prepare:** none. As with split, the sheet is the confirmation.
- **Run, under the one-action latch:**
  1. One fresh complete read. The target must be live **and placed by the document** in that space and tab (the same check as split: `CorePaneBlock`). Otherwise `not_found` or `not_applicable`.
  2. Folder = the target's `foreground.cwd`, else its `cwd`. If it no longer exists → `folder_missing` ("This folder doesn't exist."), with nothing sent.
  3. `terminal.run {"cwd": folder, "labels": {"herdr.space": S, "herdr.tab": T}, "keep": true}`, with no `argv`, `name`, `env` or `parent`:
     - `keep: true` matches every app terminal, and the app sets it on adoption anyway;
     - it means the terminal behaves the same whether or not the app ever opens.
  4. Subscribe first, then wait (`splitWait`, 5 s) for the **published** Home to contain the new address in S/T. Rule 1b (section 4) puts it there on the next read. Answer `succeeded` with the target, and the browser opens it, as split does today.
- **Errors:**

  | What happened | Outcome |
  |---|---|
  | dial failure | `herdr_unavailable` |
  | JSON-RPC error (`spawn_failed`, `invalid_params`) | `herdr_refused` with the message |
  | written but no reply, or the wait ran out | unknown |

  There is no retry.

### 3.3 New space, window closed

- **Fields:** the folder only (H2). `~` is expanded and the folder checked, as today.
- **Ids:** `"sp" + 32 hex` and `"tb" + 32 hex` from `crypto/rand`, the app's shape. Refuse to use an id already in the fresh read (never expected).
- **Run:** `terminal.run {"cwd": folder, "labels": {"herdr.space": id, "herdr.tab": tab}, "keep": true}`. `succeeded` once the reply has an address, then one refresh. Errors as in 3.2.

### 3.4 Hazards

| Hazard | What happens | Handling |
|---|---|---|
| **Stale ids** (a space or tab the person closed in the app) | Labelled ids for a closed space or tab bring it back unnamed (C6, E2, R1). | New space uses fresh ids. New terminal takes S/T from a fresh read right before the run, and **while the window is closed the document can't change**: it stayed at v6 through every closed run, and only the app writes it. A stale browser Home is refused (`not_found` / "This space changed…"). Left over: the window opens **and** the person closes that space or tab in the milliseconds between the read and the run. Accept and document. |
| **Window opens mid-action** | The app adopts within ~0.7 s and doesn't change its view or focus (E3). | Nothing to do. Home moves from rule 1b to rule 1 in one update. |
| **Two creates before the app opens** | Both are adopted. Several new spaces, or several new terminals in one tab, are appended in the order the app finishes checking them. That order varies from run to run, and it is not creation order, address-string order or space-id order (section 2). | Home shows creation order while waiting, then follows the document in one update. The warning says the position may change. The one-action latch serialises the creates. |
| **App open on another session** | (a) The default-home app runs on another core: `status` says open, so the app route is used, as in 3b today. Unchanged risk, recorded in 3b. (b) An app at another home runs on **this** core: `status` says closed, so best effort is offered, and that window adopts at once (E3). The warning overstates; the result is right. (c) The app later opens on another core: it never adopts. | Documented. No change. |
| **App never opens again** | The terminal keeps running in core. `keep` restarts a lost shell, and Home keeps showing it in its space. | Close terminal and Close space work on it. Nothing is cleaned up automatically. |
| **Target's folder gone, or `~`** | Core refuses and uses up an address (C8/C9). | Checked before the run. |
| **A waiting terminal's labelled tab ends meanwhile** | Rare. On reopen the app may drop the dead tab first and then re-create it after the selected tab. | The position may differ; it is still placed. |

---

## 4. Home in the meantime

### 4.1 The rule (added to herdr-core-plan section 3.3 as rule 1b)

> **1b.** A live terminal the document doesn't place, with non-empty `herdr.space` **and** `herdr.tab` labels, is shown where the app will put it when the window opens:
> - the space and tab are in the document → in that tab, after the tab's placed terminals, in `created_ms` order;
> - the space is in the document, the tab isn't → a tab with that id at the end of that space;
> - the space isn't in the document → a space with that id after the document's spaces, in order of its first terminal's `created_ms`, one tab per tab id. It is unnamed, so the existing label rule applies: its first terminal's `where`, else the base name of that terminal's starting folder;
> - otherwise (the tab id belongs to another space, or the space id is `other`) → unchanged, under **Other terminals**.
>
> Such a terminal counts as placed for rule 2, so its workers follow it. A waiting space offers **Close space** but not **Rename space**. Its terminals offer **Close terminal** but not **New terminal**, because they have no pane yet.

Implementation is one pre-pass in `ProjectCore`. It appends these to the parsed `spaces` and `tabs` before today's code runs, so rules 2–5, counts, notifications and Close facts work unchanged. It also predicts terminals labelled by agents following the app skill, which the app adopts anyway.

### 4.2 No visible "waiting" state

The space and terminal are real and running; only their place in the app is predicted. Recommendation (H5): show them like any other space or terminal, with no marker. AGENTS.md keeps bookkeeping out of the interface, and the warning was given before confirming.

### 4.3 How it reconciles (real reads, R3; `evidence/R3-projection.txt`)

| Read | Document | Home with rule 1b | Today's Home |
|---|---|---|---|
| after the runs | v14 | alpha / main: `t1 t18 t2 t6 t17` **`t1b`**; … ; **sub / 1: `t19 t1c`** (last) | `t19 t1b t1c` under Other terminals |
| app open, first write | v15 (t1b placed; t19's `where` = `repo / 1`) | alpha / main unchanged; **repo / 1: `t19 t1c`** | `t19 t1c` under Other terminals |
| second write | v16 (t19 placed) | alpha / main unchanged; repo / 1: `t19 t1c` (same place) | moved into **repo / 1** |

- **There is no reconciliation step.** Rule 1 is applied first, and rule 1b only covers what rule 1 doesn't. The app doesn't change `herdr.space` or `herdr.tab` on adoption (validated: only `where` and `keep` change). So no complete read can show a terminal twice or not at all, and each change is one complete Home, under the existing one-read latch. It adds no queue, replay or memory of earlier reads.
- **What can change once, at the reopen:**
  - a new space's **name**: folder base name → the app's git-top name, through `where`. In R3 this happened one read before the placement;
  - the **order of several new spaces, or of several new terminals in one tab**, created while the window was closed (the app's order varies from run to run; see section 2);
  - the **position of a re-created tab** (stale or third-party ids only).

  A single new terminal in an existing tab stays in place.
- **Notifications:** **Space opened** fires once when the waiting space first appears. Adoption keeps its id, so it doesn't fire again. Core uploads are keyed by space id, which is stable through adoption.

### 4.4 Alternative, if rule 1b is not wanted

Keep today's Home. The new terminal or space appears under **Other terminals** until the app opens, then moves. Result copy: "It's under Other terminals until the Herdr app opens." New space would then show no space at all, and the person's terminal would jump on reopen, so I don't recommend it.

---

## 5. Copy (in ui-direction's voice; for human approval)

**Where:** on the action sheet, before confirming, as a `home-action-warning` paragraph, the existing style used for "Agents may be interrupted: …".

**New space sheet, window closed** (title **New space**):
- "Add a space for an existing folder." (unchanged)
- Warning: **"The Herdr app is closed. Shepherdr will start this space in Herdr Core, and the app will add it when it next opens. Its name and place in the list may change then."**
- Field: **Working directory** only (no Label; H2). Button: **Create space**.
- Result: **Space created**, "It's on Home now. The Herdr app will add it when it next opens."
- Unknown: unchanged, "Result unknown. Check Home before starting another space."

**New terminal sheet, window closed** (title **New terminal**; today's open sheet is "Split terminal"):
- "This starts a terminal next to {terminal} and opens it."
- Warning: **"The Herdr app is closed. Shepherdr will start this terminal in Herdr Core, and the app will add it at the end of this tab when it next opens."** The position is deterministic (C1, R3), so it says "may" only for spaces.
- Buttons: **New terminal**, **Cancel** (no Beside/Below; H4).
- Result: the new terminal opens, as split does today. Unknown: "Result unknown. Check Home before starting another terminal."

**Refusals:**
- "This folder doesn't exist." (existing) also covers a New terminal whose folder is gone.
- `app_closed` on **New space** and **New terminal**, sent when the open-window sheet was confirmed after the window had closed: **"The Herdr app window is closed. Open the action again to start it in Herdr Core."** (H6)
- **Rename space** keeps "The Herdr app window is closed. Open it on the computer, then try again."

**ui-direction "With the Herdr app"** (proposed diff):

```diff
-… **New space**, **New terminal** and **Rename space** appear only while the Herdr app window is open, because the app does them. …
+… **Rename space** appears only while the Herdr app window is open, because the app does it. While the window is closed, **New space** and **New terminal** still start in Herdr Core, with a warning, and the app adds them when it next opens; until then Home shows them where the app will put them. New space then has no label, and New terminal adds the terminal at the end of its tab. …
```

**README**, the "With the Herdr app" sentence: "With the Herdr app window open, you can also make a space, split a terminal and rename a space; with it closed, you can still make a space or a terminal, and the app adds them when it next opens."

---

## 6. Wave 3c (one worker; independent review; integrator)

- **Base:** `03455ea` or the approved integrated commit after it.
- **Scope:** sections 3 and 4, the copy in section 5 as approved, and the doc changes below.
- **Likely files:**
  - `internal/herdr/core_client.go`:
    - `Run(ctx, cwd, space, tab)` with exact error kinds;
    - parse `foreground.cwd`;
    - `terminal.run` in `coreRequiredMethods` (present in 0.1.15).
  - `internal/herdr/core_project.go`:
    - the rule 1b pre-pass;
    - actions per window state (create and New terminal whenever the app client exists; Rename only when open; none of these on waiting spaces or terminals);
    - `app_window`.
  - `internal/herdr/core_client.go` `CoreRead`: the window state becomes three-way (no app client, open, closed), in place of `AppWindowOpen bool`.
  - `internal/herdr/core_projector.go`: pass the three-way state.
  - `internal/herdr/types.go`: `Home.AppWindow`.
  - `internal/server/core_actions.go`: the `best_effort` branches of `runCreateSpace` and `runSplitTerminal` (direction absent); id generation.
  - `web/src/home-parser.ts`, `home-model.ts`: `app_window`.
  - `web/src/home-view.ts`: the closed sheets, warnings, results and the `app_closed` copy.
  - `web/src/workspace-actions.ts`, `terminal-actions.ts`: `best_effort`, and `split_terminal` without direction.
  - Tests:
    - Go projection tests built from `evidence/R3-reads.jsonl` and `C-runs.jsonl` (real JSON);
    - a few web sheet tests;
    - no stand-in app.
  - A `web/dist` rebuild.
  - Docs:
    - herdr-core-plan section 2 (`terminal.run` moves out of "Never used": now used only for these two labelled runs), 3.3 (rule 1b), 3.5, 4b and 7;
    - herdr-core-actions-plan, a status note pointing here;
    - ui-direction;
    - README;
    - CHANGELOG.
- **Acceptance** (isolated core 0.1.15, app 0.1.10 and TUI 0.9.5, as earlier waves; production build; headless Chromium at a phone viewport with the memory-capped entry; then a real phone):
  1. **Window closed:** New space and New terminal show with their warnings. New space has no Label. Rename space is absent. Window open: today's sheets, unchanged.
  2. **New terminal, closed:** from a terminal whose shell has moved with `cd` into a subfolder:
     - the new terminal opens on the phone in that subfolder;
     - Home shows it at the end of that tab at once;
     - on reopen, the app shows it as the tab's last pane, and Home doesn't move it.
  3. **New space, closed:** at `~/…` and at an absolute path:
     - Home shows the space at the end at once, and **Space opened** fires once;
     - on reopen, the app shows it unnamed at the end;
     - Home changes at most its name, in one update, with no second notice.
  4. **Two New spaces, then reopen:** Home ends in the app's order.
  5. **Worker:** a worker started from a waiting terminal shows under it, and stays there after reopen.
  6. **Stale and refusals:**
     - a browser holding a Home from before the person closed that space in the app → "This space changed…" or not found, and nothing comes back on reopen;
     - a missing folder → "This folder doesn't exist.", with no core address used.
  7. **Mid-action:** confirm, then open the app at once → placed within about 1 s, and the window's view is unchanged.
  8. **Never opened:** Close terminal and Close space on waiting ones work, and nothing reappears on reopen.
  9. **Window closes during an open-window sheet** → the new `app_closed` copy, and reopening the action shows the closed sheet.
  10. **Unchanged:** Wave 3a and 3b acceptance still pass, TUI is unchanged, and hostile folder names render as inert text.
  11. **Isolation record:** the human's `terminals.json` hash before and after.
- **Gate:** independent review approves; the integrator runs checks 2, 3, 6 and 10 on the integrated commit; the human accepts on a real phone.

---

## 7. Risks

- **Undocumented detail.** The opt-in label is documented. Position, order, unnamed naming, `keep` and focus behaviour come from 0.1.10's source and these runs, and are validated per app version, like the rest of 3b.
- **Home predicts placement.** A one-time name or order change on reopen (section 4.3) is accepted and stated in the warning.
- **Labelled terminals from other tools** move from Other terminals into their labelled space. That is where the app puts them on open.
- **An app at another home on the same core** makes "closed" wrong. The result is still right.
- **A refused run uses up a core address.** This is cosmetic, and mostly avoided by the folder check.

---

## 8. Decisions (approved 2026-10-31)

1. **H1. Home rule 1b:** waiting terminals show in their space and tab, and a new space shows at once. **Recommended.** The alternative is section 4.4.
2. **H2. New space label while closed:**
   - (a) **No Label field; rename once the app is open. Recommended.**
   - (b) Keep the field and show the name only until the app opens. The app never uses it, so the name would change to the folder's.
   - (c) Shepherdr renames the space when the window next opens. This needs a deferred-action watcher, which AGENTS.md rules out.
3. **H3. Copy** in section 5.
4. **H4. New terminal while closed:** one **New terminal** button (no Beside/Below), and it opens the new terminal. **Recommended.**
5. **H5. No "waiting" marker** on Home. **Recommended.**
6. **H6. New `app_closed` copy** for the two create actions.
7. **H7. Doc changes:** herdr-core-plan sections 2, 3.3, 3.5, 4b and 7; ui-direction; README. Above all, reversing "never use `terminal.run`" for these two labelled runs, with its stale-id rule (fresh read; the document can't change while closed).


