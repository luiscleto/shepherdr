# Shared files proposal

Status: **approved by the human on 2026-11-04**, with the changes in "Approved direction" below. Where the rest of this document disagrees with that section, that section wins.

## Approved direction (2026-11-04)

- **Decisions.** S1–S9 and U1–U8 are approved as recommended (U8: Markdown shows as plain text). The S8 notification is part of the work, off by default.
- **New count, not total.** The Shared files icon's number counts files nobody has listed yet, not files kept. Showing the Shared files list marks every file in it as seen; opening, previewing or downloading isn't needed. Seen is shared by every device, like Herdr's seen: listing on one device clears the number on all of them. On a Terminal page, the icon counts that terminal's unseen files, and its filtered list marks only those seen. With no unseen files the icon shows no number. The server keeps the seen state with the index; the browser only says which list is in view.
- **Terminal identity.** Herdr TUI reuses pane ids (and terminal ids may repeat), so a record's terminal key must never match a later, different terminal. Record the claimed id together with something that is never reused for another terminal, such as the terminal process's id plus its start time, and use that key for the filter, the badge, the notice and **Open terminal**.
- **Separate folders.** Shared files and uploads stay in separate folders with separate flags and cleanup rules: uploads in temp, cleaned up when the space closes; shared files in `-share-dir`, kept until deleted. Agents can't write to `-share-dir`.
- **Agent discoverability.** Two layers, provider-neutral:
  - `shepherdr --skill` prints a short note for agents (what `shepherdr share` is, when to use it, and that `shepherdr share --help` has the details), following `hc --skill`. Settings shows the same note in a **For your agents** block with **Copy**, saying to paste it into global agent instructions such as `~/.claude/CLAUDE.md` or `~/.codex/AGENTS.md`. Shepherdr never edits those files.
  - On a Terminal page, the Shared files header ⋮ menu (filtered to that terminal), and that list's empty state, have **Ask for a file**. It returns to the terminal and puts a ready message in the composer without sending it, e.g. "Please share it with me in Shepherdr: run `shepherdr share <file>` (see `shepherdr share --help`)."
- **Composer kept.** A Terminal page's unsent message and picked files survive a visit to Shared files and back, as they survive the Reader/Terminal switch; **Ask for a file** adds its message after an existing draft.
- **Out of scope.** Per-device sharing (files are already visible to every trusted device), a combined uploads-and-shares view, and a Claude Code skill.

Base: `e39e73c`. Date: 2026-11-04. Evidence and commands are in the planning report outside the repository. Screenshot names below (for example **shot 02**) refer to the mockups delivered with it. They were built from the real `web/dist` CSS and markup against an isolated Herdr TUI 0.9.5 and herdr-core 0.1.17.

## 1. Summary

The human's request: agents often make an image or a PDF the human wants on the phone. Today that means exposing another port. They want a way for agents to share files through Shepherdr, plus a **Shared files** view reachable from Home and from Terminal. The view should sort by agent, time and size, and support download and delete.

Recommendation, in one paragraph: the same `shepherdr` binary gets a `shepherdr share <file>…` command. It streams each file's bytes to the running Shepherdr over an owner-only local socket. Shepherdr keeps its own copy in a private folder until someone deletes it. It labels the copy with the terminal it came from, checking the claim against Herdr's own process IDs. Home gets a **Shared files** icon button with a count, and Terminal gets the same button filtered to that terminal, plus a dismissible notice when a file arrives while you're looking. The view lists files with sort, preview, download and delete. Files are served from Shepherdr's own origin with headers that keep their content from running as Shepherdr. Herdr itself is untouched: nothing in Herdr or herdr-core shares files this way, and Shepherdr does not parse terminal output.

## 2. UX walkthrough

Phone first (390×844). Desktop where layout differs.

1. **Home (shot 01, 01b, 12).** A tray icon with a count badge sits beside Settings in the masthead, on both the Herdr TUI and Herdr Core sides. The count is the total kept, not "unread"; there is no unread state. Its accessible name is "Shared files, 8".
2. **Shared files (shot 02, 13).** The page has a paper header with **Home**, the title, "8 files · 13.6 MB", and a ⋮ menu. Below that are a sort button and one list for both Herdrs. Each row shows:
   - a thumbnail for images and video, or a type tag (PDF, MD, TXT, ZIP);
   - the file name;
   - **From**: space / tab · agent in Herdr Core, workspace · agent in the Herdr TUI;
   - when it was shared, and its size.

   "· Herdr TUI" is added only when both Herdrs are present and the file came from the one Home isn't showing; that is a mockup simplification (decision U5). A file shared outside any terminal says **Outside Herdr**. Desktop shows the same data as columns (Name, From, Shared, Size) with an inline download button.
3. **From a Terminal (shot 03).** The Terminal header's new tray button (shot 09) opens the same view, filtered to that terminal. A bar under the header reads "From **site** · claude-code" with **Show all 8**. The header button says **Back** and returns to the terminal. The filter is the exact terminal that shared the file, so it still works after the agent exits.
4. **Sort (shot 04, 04b).** A bottom sheet offers **Newest first** (the default), **Oldest first**, **Largest first**, **Smallest first** and **By agent**. By agent groups files under the terminal that shared them ("site · claude-code · 3"), newest group first. Each browser remembers its choice.
5. **A file's actions (shot 05).** The row's ⋮ menu has **Open**, **Download**, **Open terminal** (only while that exact terminal is current) and **Delete**. Tapping the row is **Open**.
6. **Delete (shot 06, 06b).** The confirmation says "This removes Shepherdr's copy for every device. The file the agent shared from stays where it was." The header ⋮ menu has **Delete all shown (N)** for the current filter, with its own confirmation (shot 06b). It deletes several files at once without adding a selection mode.
7. **Preview (shot 07, 16, 14).** Images fill the page; long-press saves them on both phone platforms. Video uses the browser's player. Text and Markdown show as plain monospace text, not rendered. The footer carries **From …**, **Open terminal**, **Download** and **Delete**. Desktop opens the same viewer as a dialog over the list (shot 14).
8. **PDF (shot 08, 08b).** A card with **Open PDF** opens the file in the browser's own viewer in a new tab (shot 08b: real Chrome viewer). Shepherdr does not render PDFs itself (section 4.4).
9. **A share while you're looking (shot 09, 10, 15).** The Terminal header button shows that terminal's count. When a file from this terminal arrives while the page is in view, Reader's existing paper banner style shows "Shared from here: **pricing-mobile.png** and 1 more", with **Show files** and a dismiss button. It shows in Reader, the full terminal and desktop, lasts for the visit, and never covers the composer. Terminal output is not parsed. The agent's own "Shared … in Shepherdr." line stays plain text, Reader shows no links, and the existing full-terminal **Open link?** sheet is unchanged.
10. **Empty (shot 11, 11b).** "Nothing shared yet. When an agent shares a file, it appears here, newest first, until you delete it. Ask an agent to run `shepherdr share <file>`." Filtered and empty: "Nothing shared from notes", with **Show all**, and **Ask for a file** while that terminal is current. A terminal outside every space is named by its own title here, in the filter bar and in its From line.

Not in the mockups: an optional notification (decision S8) and the CLI text (section 3.4).

## 3. Architecture

### 3.1 Components

- **Share socket.** A Unix socket in an owner-only directory, at `$XDG_RUNTIME_DIR/shepherdr/share.sock` by default. When `XDG_RUNTIME_DIR` is unset (macOS), it goes under the user config folder beside `access.json`. A flag can override it. The running Shepherdr listens on it; nothing new listens on the network.
- **Share store.** It lives in the existing process and handles the share stream, attribution, the index, limits and deletes. It is similar in size to `internal/uploads`.
- **`shepherdr share`.** A subcommand of the same binary. It reads the files the caller names, with the caller's own permissions, and streams them to the socket.
- **Browser.** The Shared files view, the two entry buttons and the Terminal notice. It gets the complete list as one message on the existing Home connection, which stays open on the Terminal page.

### 3.2 Data flow

```
agent ──shepherdr share a.png──▶ CLI ──(owner-only socket: request line + bytes)──▶ Shepherdr
                                                                             │ peer pid → ancestry check
                                                                             │ against Herdr's pid for the
                                                                             │ claimed terminal (one read)
                                                                             ▼
                                         shared/<random id>/a.png  +  shared.json (index)
                                                                             │ complete list
                                                                             ▼
                                    Home connection ──▶ Home count, Shared files, Terminal badge/notice
browser ──GET /api/shared/<id>──▶ bytes with the strict headers (4.4)
browser ──POST /api/shared/delete {ids}──▶ remove copy + index entry ──▶ new complete list
```

### 3.3 Storage and lifecycle

- **Copy at share time.** The CLI opens and reads the file. Shepherdr never opens a path an agent names, so symlinks, later edits, deleted originals and path tricks can't reach the store or change what was shared. Only regular files are shared. A folder or device is refused ("Share files, not folders.").
- **Where.** `-share-dir` holds a `0700` folder with one `0700` subfolder per share, named by a random 128-bit id. The file inside is `0600` and named with the uploads' existing safe-name rules (`safeBasename`). Next to it is an owner-only `shared.json` index, written atomically with the existing local-state discipline. Each record holds:
  - id, display name, size, type tag, time shared;
  - the Herdr side and exact terminal key;
  - display labels copied at share time: space or workspace, tab, agent;
  - whether the source was checked.

  The default folder is persistent, not the temp directory: `$XDG_DATA_HOME/shepherdr/shared`, else `~/.local/share/shepherdr/shared`, or the config folder on macOS. Shared files survive restarts and reboots. Uploads use temp because they are short-lived; shares are meant to be kept.
- **Writes.** Bytes stream into `.incoming/<id>`, then an atomic rename and an index commit. Startup clears `.incoming` and drops index entries whose file is missing. The store owns its folder outright, so this is safe, unlike scanning `/tmp`.
- **Retention.** Files are kept until deleted (decision U3). They are not removed when the agent, terminal or workspace ends.
- **Limits.** `-share-limit` sets the largest single file. The recommended default is `1GiB`, matching `-upload-limit`, and `none` is allowed. `-share-total` sets the total kept, recommended `10GiB`, with `none` allowed. A share that would pass either limit is refused before any byte is stored, with a CLI message that says how much is kept and to delete some in Shepherdr. Nothing is evicted automatically. The stream has the same pace rule as uploads, cut off if it stalls.
- **Reset.** A stopped-service `shepherdr shared reset` command could clear everything. It is optional and not needed now.

### 3.4 CLI

```
shepherdr share [-socket PATH] FILE...
```

For each file it prints `Shared pricing-mobile.png (96 KB) in Shepherdr.` When Shepherdr knows its address (`-public-origin` or `-no-sign-in-origin`), it adds the file's address: `https://host.example.ts.net/#shared=<id>` (decision S7). Failures say what happened:
- Shepherdr isn't running;
- not a file;
- too large;
- shared files are full.

The exit code is nonzero on any failure. `shepherdr share --help` is the whole agent manual, about 15 lines. It covers when to share (the human asked to see a file), that the file is copied, that the human can delete it, and the limits. Agents can't list, read back or delete shares (decision S6).

**Finding the binary.** Herdr owns pane environments, so Shepherdr can't add itself to `PATH`. The README tells the operator to put `shepherdr` on `PATH`, for example a symlink in `~/.local/bin`. The ready log line prints the exact share command with the absolute path, and the README gets a three-line snippet for `AGENTS.md`/`CLAUDE.md`. There is no Claude Code skill and no `--skill` flag: provider-specific prompt handling is outside the product, and `--help` is enough (decision U6).

### 3.5 HTTP routes (added to the deny-by-default inventory)

| Route | Rule |
| --- | --- |
| `GET`/`HEAD /api/shared/{id}` | Protected route. Exact id from the index only, otherwise 404. `?download=1` sets `attachment`. Headers in 4.4. |
| `POST /api/shared/delete` | Protected mutation: exact `Origin`, `application/json`, bounded body `{ids:[…]}`. Deletes exactly those ids and reports which were gone already. |
| Home connection | Adds one `shared` message with the complete list, sent on connect and whenever the list changes. It does not resend Herdr state. |

Sign-in-off mode uses that mode's existing Host/Origin boundary, as every other route does.

### 3.6 Flags

`-share-dir` (persistent default above), `-share-limit` (`1GiB`), `-share-total` (`10GiB`), `-share-socket` (default above). Everything else stays a fixed default: id size, headers, preview types, sort list and socket permissions.

## 4. Research answers

### 4.1 How an agent shares: the `share` subcommand over a local socket

| Option | Verdict |
| --- | --- |
| **`shepherdr share` → owner-only Unix socket** | **Recommended.** Explicit, one-shot, and works the same in both Herdrs. It gives a clear error when Shepherdr isn't running, and the socket's peer credentials allow attribution. Bytes stream, so size isn't a problem. |
| Watched drop folder | No attribution: any process can write. It also raises half-written-file questions, needs a watcher or polling, and makes "delete" ambiguous (the folder or the copy?). |
| Terminal output conventions (OSC 8 `file://`, printed paths) | Rejected. Shepherdr would have to parse output and turn printed text into file access, which `AGENTS.md` and this brief both forbid. Herdr TUI's `pane.link.resolve`/`activate` resolve a link at a screen position for clicking. They are not a share channel. |
| Loopback HTTP endpoint | A local process can't present a browser session, so it would need its own token scheme. A socket's filesystem permissions already mean "same account". |
| Herdr's own features | Not exposed. herdr-core 0.1.17's 32 methods include `terminal.put` (person → terminal, up to 8 MB, "for its program to read"), `file.*` and `doc.*`. Herdr TUI 0.9.5's API has no file or share method. None lets an agent hand a file to a viewer. |

### 4.2 Who shared it

Each Herdr sets these variables in its terminals. Both were checked on an isolated Herdr TUI 0.9.5 and herdr-core 0.1.17:

- **Herdr TUI pane:** `HERDR_ENV=1`, `HERDR_PANE_ID=w1:p1`, `HERDR_TAB_ID`, `HERDR_WORKSPACE_ID`, `HERDR_SOCKET_PATH`, `HERDR_BIN_PATH`. `pane.process_info` returns `shell_pid`.
- **herdr-core terminal:** `HC_TERMINAL_ID=t5`, `HC_TERMINAL_SOCKET`, `HC_SESSION`, `HC_SCOPE`, `HC_RUNTIME=1`, `XDG_RUNTIME_DIR`. `terminal.get` returns `pid`. Its API also trusts a caller-declared `caller` (= `HC_TERMINAL_ID`).

The recommended rule is **claim, then check**:

1. The CLI sends the claim from its environment: which Herdr (by socket path) and which pane or terminal.
2. Shepherdr accepts only a claim naming a socket it follows.
3. It reads the peer's process ID from the socket (`SO_PEERCRED` on Linux, `LOCAL_PEERPID` on macOS).
4. It walks the peer's parent chain and requires the claimed terminal's process, from one fresh Herdr read, to be an ancestor.

A live check showed the chain reaching the terminal's own process in both Herdrs: TUI `34151:ancestry.sh → 8173:sh (= shell_pid)`, core `34312:ancestry.sh → 17957:sh (= pid)`. If the check fails or there is no claim, the share is still stored and shows **Outside Herdr** (decision S2). Examples are a share from a plain terminal, a daemonized helper or `setsid`. A worker started with `hc run --bg` has its own terminal and is attributed to that worker.

**How far to trust it.** The check stops an honest-but-confused process, or a copied environment, from mislabeling itself. A same-account process can still impersonate a terminal, for example by running a child under that terminal's process. The label is display text only. It never chooses a target, grants access, or limits who can delete (decision S2).

### 4.3 Storage and lifecycle

See 3.3. Copying beats keeping a reference to the original. The phone gets exactly the bytes shared at that moment, and a later edit, move or delete of the original can't change or break it. Shepherdr also never has to reopen agent-chosen paths, so it needs no path-traversal or symlink policy. The cost is disk space, which the total limit and **Delete all shown** handle.

### 4.4 Viewing and downloading safely

Shared bytes are untrusted, and serving them from Shepherdr's origin is the risk. Every file response gets the same fixed headers, whatever the file claims to be:

- the `Content-Type` comes from Shepherdr's own allowlist by extension; anything else is `application/octet-stream`. Content is never sniffed;
- `X-Content-Type-Options: nosniff`;
- `Content-Security-Policy: sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'`;
- `Cross-Origin-Resource-Policy: same-origin`, `Cache-Control: private, no-store`, and the existing frame and referrer headers;
- `Content-Disposition: attachment` for every type outside the preview allowlist, and always with `?download=1`. RFC 5987 filenames are built from the safe name.

Checked in headless Chromium 155 against a throwaway server:

| Case | Result |
| --- | --- |
| HTML served as its type, no sandbox | script ran |
| HTML with the `sandbox` CSP | script did **not** run |
| SVG opened directly, no sandbox | script ran |
| SVG with the `sandbox` CSP | script did **not** run |
| SVG in an `<img>` on a page | rendered, no script |
| PNG and SVG with `attachment` + `sandbox`, loaded by `<img>` | both still load |
| PDF top-level, with and without `sandbox` | both rendered in Chrome's viewer |

So one strict header set covers every type, and previews can still use `<img>`.

Preview allowlist (decision S5):
- **images** (PNG, JPEG, GIF, WebP, AVIF, and SVG only inside `<img>`) through `<img>` in Shepherdr's page, which already allows `img-src 'self'`;
- **video** (MP4, WebM, MOV) through `<video>`;
- **text** (`.txt`, `.md`, `.log`, `.json`, `.csv` and similar, up to 1 MiB shown) fetched and inserted as `textContent`, like other untrusted text;
- **PDF** top-level in a new tab, opened in the browser's own viewer;
- everything else, HTML included: download only.

Shepherdr does not bundle a PDF renderer. pdf.js in Shepherdr's origin would parse untrusted PDFs with Shepherdr's authority, and pdf.js had an arbitrary-script bug in 2024 (CVE-2024-4367). Shepherdr's own page CSP gains only `media-src 'self'`.

**Phones.** Download is a same-origin link with `?download=1`. The host-only `SameSite=Strict` session cookie goes with it because it starts on the same site. Android Chrome saves to Downloads. Long-press saves an image on both platforms. iOS Safari shows its download prompt. In an iOS home-screen app, downloads and new tabs behave differently: a new tab may open in Safari, which has its own cookies and would need sign-in. This could not be tested here, so it is an acceptance check and a risk. A system **Share…** button (`navigator.share({files})`) for saving to Photos or Files is left out for now (section 6).

### 4.5 Reader and terminal tabs

Doable without touching terminal output. The badge and notice come from Shepherdr's own records for the exact terminal the page shows (shots 09, 10, 15). Only an id in the index is reachable, so text an agent prints can never open another file. A printed Shepherdr address (decision S7) opens only through the existing **Open link?** sheet, in a new tab, and can only reach a file that was actually shared, which the person can already see.

## 5. Decisions for the human

### Security (yours to approve)

**S1. Who may share.**
- (a) Any process of the same OS account, through the owner-only socket.
- (b) Only processes inside a checked Herdr terminal.
- (c) (a) plus a token the operator hands out.

(b) blocks sharing from a plain terminal and from daemonized helpers. (c) adds a secret for no real gain, because the same account can read it. **Recommend (a).** It matches the existing "same OS account is trusted" assumption in the passkey and uploads documents.

**S2. Attribution.**
- (a) Claim, then check (4.2); a failed check shows **Outside Herdr**.
- (b) Trust the environment claim as is.
- (c) Refuse unchecked shares.

**Recommend (a).** The label is display-only and never authority.

**S3. Local channel permissions.** A socket in a `0700` directory owned by the user. Shepherdr refuses a directory with the wrong owner, wrong mode or a link, and refuses peers whose uid differs. At startup it removes a stale socket only after a connect fails. **Recommend as stated.** The only alternative is a world-reachable path, which no one needs.

**S4. Who can see and delete.**
- Protected mode: every trusted sign-in sees and can delete every shared file, with no per-device ownership.
- Sign-in off: anyone who can reach Shepherdr can see, download and delete them, as with everything else.

The alternative for sign-in off is to disable Shared files there. **Recommend: consistent with the existing operator-authority rule.** The README notes that shared files are reachable by anyone who can reach Shepherdr.

**S5. Preview and serving rules.** The fixed header set and preview allowlist in 4.4: no HTML or SVG rendered as a document, Markdown as plain text, PDFs only in the browser's own viewer.
- Alternative: a separate origin or port for files. That is stronger isolation, but it needs a second private HTTPS name and its own cookies.
- Alternative: no previews, download only.

**Recommend 4.4 as written.**

**S6. What agents can do.**
- (a) Add only.
- (b) Also list and delete their own shares.

(b) widens a one-way channel into a read and delete API for every local process. **Recommend (a).**

**S7. The printed address.**
- (a) The CLI prints `…/#shared=<id>` when Shepherdr knows its address.
- (b) Never print one.

Ids are 128-bit random and lead only to files in the list. Printing one puts a Shepherdr address in terminal history and agent transcripts. **Recommend (a).** It gives a one-tap path in the desktop terminal through the existing **Open link?** sheet.

**S8. Notification.** A new **File shared** event, off by default.
- Payload, option (a): "File shared" plus the space or workspace name, and the tab name the person gave, like status notices. No file name.
- Payload, option (b): also the file name. File names are agent-written content, which notification payloads exclude today.

**Recommend (a), off by default.** Tapping it opens Shared files filtered to that terminal.

**S9. Persistent copies.** Shared files persist in a private data folder until deleted, including files an agent shares by mistake, such as a key. Alternatives are a temp folder (lost on reboot) or an age limit. **Recommend persistent plus delete.** The README states where the folder is.

### Product and interface

**U1. A third screen.** `docs/ui-direction.md` says "Do not add another main screen". **Recommend** amending it to allow **Shared files** as a page reached from Home and Terminal, like Settings is.

**U2. Entry placement.** The options are:
- (a) a tray icon with a count beside Settings on Home and in the Terminal header (shots 01, 09);
- (b) a text button "Shared files 8" in Home's tools row;
- (c) an item inside Settings.

(b) is clearer but takes a row. (c) is hidden. **Recommend (a).** Its accessible name and tooltip are "Shared files".

**U3. Retention.**
- (a) Keep until deleted.
- (b) Delete after an age set by `-share-keep`.
- (c) Delete when the terminal or workspace ends.

(c) loses files the human still wants after the agent finishes. **Recommend (a)** with the total limit.

**U4. Defaults.** `-share-limit 1GiB`, `-share-total 10GiB`. **Recommend.** Check `10GiB` against the machine's disk.

**U5. One list for both Herdrs.** Show both in one list, or follow Home's **Herdr TUI | Herdr Core** switch. **Recommend one list.** Files outlive terminals and the switch, and the From line names the place. Mark a file from the Herdr Home isn't showing only when both are present.

**U6. Agent help.** `--help` plus a README snippet, or also a `shepherdr --skill` flag like `hc`/`herdr`. **Recommend `--help` and the snippet only.**

**U7. Sort set.** Newest, Oldest, Largest, Smallest, By agent, with no name sort and no search. **Recommend.** The Home filter has no counterpart here at first.

## 6. Deliberately left out

- Selection mode with checkboxes. **Delete all shown** covers bulk cleanup.
- Rename and edit.
- Folders and zip upload: share a zip.
- Sharing from the phone to another device.
- Public or expiring links.
- Search.
- Markdown or HTML rendering.
- An in-page PDF renderer.
- Thumbnails made on the server. Thumbnails are the image itself, scaled by the browser; video uses the browser's first frame or a type tag.
- A system **Share…** button. A later candidate for iOS home-screen downloads if acceptance shows it's needed.
- Unread state, history and delivery claims.
- Agent list and delete.
- A Claude Code skill.
- Watching folders.
- Changes to Herdr.

## 7. Risks

- **iOS home-screen app downloads** may open in Safari without the session, or show only a preview. Needs a real iPhone check. A system **Share…** button is the fallback.
- **Agent sandboxes.** Claude Code's sandboxed Bash may block Unix sockets or hide `XDG_RUNTIME_DIR`. If so, `shepherdr share` fails with "Shepherdr isn't running". That needs the sandbox's own Unix-socket setting, as `hc` would. The sandbox could not be exercised here.
- **Attribution gaps.** Daemonized or `setsid` helpers, `tmux` inside a pane, and remote `hc --on` machines show **Outside Herdr**, which is honest but less useful.
- **Disk use.** Persistent copies grow until deleted. The total limit refuses new shares rather than evicting old ones.
- **Viewer bugs.** PDF and image decoders are browser code running on untrusted bytes. The sandbox CSP limits a bug to the browser's own process model, not Shepherdr's authority.
- **Two Shepherdr instances** on one account would clash on the socket. The second one refuses to start sharing and logs it. `-share-socket` separates them.

## 8. Real acceptance checks

Production executable with a real Herdr TUI and herdr-core, and real Claude Code and Codex agents. Protected mode once and `-no-sign-in` once. A real Android phone, and an iPhone in Safari and as a home-screen app.

1. An agent in a Herdr TUI pane and one in a Herdr Core tab each run `shepherdr share` on a PNG, a PDF, a Markdown file, an MP4 and a 600 MB file. Each appears on Home's count, in Shared files with the right From line and size, and on that Terminal's badge. The notice shows only on the open Terminal page for that terminal.
2. A share from a plain shell outside Herdr, a `setsid` child, and a forged `HERDR_PANE_ID`/`HC_TERMINAL_ID` in a process not under that terminal all show **Outside Herdr**. An `hc run --bg` worker shows as itself.
3. Sort by each choice, filter from Terminal, **Show all**, back to the terminal, and **Open terminal** for a current terminal. That action is absent once the terminal is gone.
4. Preview image, video, text and PDF. Download each type on Android Chrome, iOS Safari and the iOS home-screen app; record only what the phones actually did. Long-press save an image.
5. Hostile content: share `evil.html` and `evil.svg` whose script tries `fetch('/api/home')`, and `evil.png` that is really HTML. Open each by preview, download and direct address. Confirm no script runs and nothing reaches Shepherdr data.
6. Hostile names: controls, bidi characters, `../`, very long names and duplicates. Confirm safe display names, separate copies and an intact store.
7. Delete one, **Delete all shown**, and delete from a second device while the first views the list. The list updates live everywhere, and a deleted id returns 404.
8. Limits: per-file and total refusals happen before bytes are stored, with the CLI message. A killed CLI mid-stream leaves no file and no index entry. Restart Shepherdr and reboot: files and labels survive, and `.incoming` is cleared.
9. Shepherdr stopped: the CLI says so and exits nonzero. Socket directory with the wrong mode: Shepherdr refuses to listen.
10. With the notification on: one notice per share burst, the payload as decided in S8, and a tap opens the filtered view.

## 9. Suggested work split

One worker at a time, as `AGENTS.md` prefers until the loop is reliable.

- **Wave A: share store, socket and CLI (server only).**
  - Base: the approved integrated commit after this proposal.
  - Scope: `internal/shares` (store, index, limits, startup clear), the socket listener and peer check, attribution against both Herdrs, `shepherdr share`, the flags, `GET /api/shared/{id}` with the 4.4 headers, `POST /api/shared/delete`, and the `shared` message on the Home connection.
  - Tests: small Go tests on real temporary folders.
  - Gate: acceptance checks 1 (CLI half), 2, 5 (direct address), 6, 8 and 9 through curl and the real CLI against isolated Herdrs.
  - Review: independent reviewer. Integrator: separate.
- **Wave B: browser.**
  - Base: Wave A's integrated commit.
  - Scope: Home and Terminal buttons, the Shared files view, sort, filter, preview, download, delete, **Delete all shown**, the Terminal notice, and the page CSP `media-src`.
  - Tests: memory-capped browser tests comparing primitive values.
  - Gate: acceptance checks 1, 3, 4, 5, 7 on real phones.
- **Wave C: optional pieces, only if approved.**
  - The S8 notification, the README section and snippet, the CHANGELOG, and `docs/ui-direction.md` / `docs/north-star.md` amendments for U1.
  - Gate: check 10 and a README read-through.

Overlap: Wave A touches `main.go` (subcommand and flags), `internal/server` (routes, Home message) and `access_boundary.go` (route inventory). Wave B touches `home-view.ts`, `terminal-page.ts`, `main.ts` and `style.css`. They are sequential, so they don't conflict.
