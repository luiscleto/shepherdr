# Send keys plan

Status: **approved human direction and implementation brief**. Human approval covers Herdr `pane.send_keys`, retained control, **all confirmed public key capabilities**, and the documented final-check and cross-path ordering limits. No xterm encoder imports, custom byte encoder, or mode negotiation. Main dispatches implementation; this is not launch approval.

Audit base: `213695cce8ccd26cc87b70761387c2f6b0c71a2c`, native worktree `plan/send-keys`; master remains unchanged. **This document is the single work brief. Its governing commit is the exact worker dispatch base**, which main records after committing; no duplicate brief or self-referential SHA is needed.

Independent reviewer **keys-banner-reviewer** approved the technical proposal without blocking findings at plan SHA-256 `6f1d0e4a3360f2401622ef6cbf64cb68da0b263826b1e79db289e511c736fc7c` and UI SHA-256 `d82891d42eeb44e3274919ad322938e8bbd6ae1e4358cbfb21086476438e8110`. This update records subsequent human acceptance and ownership without changing scope.

## Scope and UI

Follow [approved UI](ui-direction.md#send-keys), [Terminal](terminal-direction.md), [retained control](mobile-terminal-shared-trial-proposal.md#accepted-behavior), and existing [file](mobile-terminal-file-uploads-architecture-proposal.md) / [access](passkey-device-architecture-proposal.md) boundaries except the explicit key-send changes below.

Use the approved paper sheet: Ctrl, Alt/Option, Shift, plus confirmed Super/Command and Hyper modifiers; Common/F keys/Character; one base plus modifiers; readable preview and explicit Send; pin without sending; browser-local edit/remove/reorder and Restore defaults. **Send keys is text-only, immediately before Release.** Default optional shortcuts are Esc/Tab; no arbitrary saved-count cap. Alt/Option means terminal Alt. Keep the same three categories and wrap modifier controls as needed; no additional category is necessary. Keep macros, profiles, global sticky modifiers and provider logic out.

Viewed all five revised PNGs in `/tmp/shepherdr-keys-study.eVomdn/` (`01` bar, `03` Common, `02` F keys, `04` Character/keyboard, `05` editing), accompanying the [approved study](https://stitch.withgoogle.com/projects/7610091999668176410). Human corrections override incidental image text/geometry.

Current inventory from [TerminalPage](../web/src/terminal-page.ts) and [terminal-input](../web/src/terminal-input.ts), in bar order:

| Current action | Disposition |
| --- | --- |
| Message / full-Terminal Attach files | Fixed; same composer/files and agent availability |
| Control / Take over / Release | Fixed, lifecycle-dependent; existing confirmation |
| Esc | Common picker + optional shortcut; default saved |
| Enter | Fixed + Common picker |
| Left, Up, Down, Right | Each an explicit Common key + optional shortcut |
| Ctrl C, Ctrl D, Ctrl Z | Each reachable through Character + Ctrl + optional shortcut |
| Tab | Explicit Common key + optional shortcut; default saved |
| Backspace | Explicit Common key + optional shortcut; independent of phone keyboard |

Keep Home, Settings, Reader/full-Terminal switch, reading/selection/copy/history, native typing/paste/scroll, composer Send/close, Add files/Photos/Files/removal, Insert files/cancel and existing message/file recovery in their own surfaces. Anything not replaceable by a key stays fixed. Fixed Enter and optional key buttons use the same key-send operation; native typing and Message/file submission retain their existing paths.

## Approved implementation

**Add one narrowly validated key command to the existing terminal WebSocket.** Use its bound target/sign-in, request IDs, sequential input handler and per-controller `writeMutex`; call the existing Herdr client for one `pane.send_keys` RPC. No new browser endpoint, transferable handle, queue, scheduler, or controller lifecycle.

1. Browser sends one base/modifier selection and request ID, never a destination chosen by a saved shortcut. Server validates the confirmed base and Ctrl/Alt/Shift/Super/Hyper flags and constructs one canonical Herdr key string (`keys` contains exactly one entry). Use aliases for space/plus, preserve Herdr's implicit Shift for uppercase ASCII, and reject empty/invalid characters, multiple bases, injected separators and parser-only function names. A byte containing zero (Ctrl+Space) is valid input, not an empty encoding. Herdr owns byte encoding.
2. In the existing input handler, require a production target lease and an established, still-live controlling child. **Reject observer, released, pending-acquisition and ended connections on the server**, even for forged browser commands. Under the existing write lock, check cancellation, `session.controlled`, child lifetime and target lease; obtain one fresh Herdr snapshot and verify the bound workspace/tab/pane still names the exact terminal. Recheck control/lifetime/lease and current sign-in authority immediately before RPC submission using the existing commit-authority guard. No fallback target or recognized-agent requirement for keys.
3. Keep that lock through the bounded key RPC, as the smallest coordination with local text/file forwarding and Release. Use existing client deadlines and cancellation semantics; keep frame/heartbeat handling free. Known control loss must prevent subsequent keys. Never acquire, release, focus, resize or reconnect as part of sending a key.
4. Return one request-correlated key outcome through the socket: accepted by Herdr, definitely not sent, or unknown after possible submission. Reuse existing outcome classification concepts and single-action tracking, with key-specific completion that cannot run Reader submission callbacks. A refusal alone must not restart the terminal. No automatic retry or replay; after an unknown result, require the existing deliberate check-before-resending recovery.

Source fit: [bridge input handler](../internal/server/terminal_bridge.go), [shared write lock/batches](../internal/server/terminal_batch.go), [target lease](../internal/server/terminal_target.go), [authority guard](../internal/server/access_boundary.go), [Herdr mutation client](../internal/herdr/actions.go). Add a narrow `SendKeys` client method and key-command/result validation; preserve existing sign-in-off Host/Origin/operator rules and protected-session cancellation. Do not nest authority locks or create another runtime.

**Keys act on the current remote terminal state, unrelated to unsent Reader text/files.** Do not read, submit, clear, close or refocus the Reader draft when sending a key. Reuse actual in-flight/unresolved-input exclusion, not draft-content gating; mere pending text/files must not block keys. Modifiers affect only the chosen key, never native typing, fixed Enter or another shortcut. Selection, pin/edit/remove/reorder/restore produce no input or acquisition. Saved preferences contain only validated ordered combinations, with visit-local fallback if storage fails.

While observing, use the existing explicit Control or confirmed Take over action first, then deliberately send the key. **No per-key takeover or Take over and send coupling.** Existing Message/files recovery remains unchanged. Disconnection disables key sending and preserves local choices.

## Accepted limits

| Limit | Consequence and comparison |
| --- | --- |
| RPC accepts only pane ID, not expected terminal/controller | A replacement or external takeover after the final check can still receive the key. This differs materially from old shortcuts written to an attached terminal stream: the RPC does not enforce that stream's final target/control binding. Similar check-then-act windows exist in management actions, but that is not an atomic guarantee for keys. |
| Key RPC and stream input are separate writes | The shared lock prevents overlapping Shepherdr dispatch within this controller, including file batches and Release. It cannot prove global arrival/consumption order with already-forwarded text or another caller. Earlier stream “forwarded” acknowledgement is not a drain barrier. This is a new ordering limitation versus one stream. |
| Herdr success means input queued | It does not mean the application consumed or acted on the key. This remains consistent with existing non-delivery claims. Timeout/disconnect after possible submission is unknown, never automatic resend. |

**Human explicitly accepts these limits**, including a key arriving after a final-check takeover or terminal replacement and the lack of guaranteed cross-path input ordering. Keep the specified server guards and truthful outcomes; no additional coordination machinery or unresolved approval remains for these accepted risks.

## Capability evidence

Independent reviewer **keys-banner-reviewer** confirmed Herdr 0.9 `pane.send_keys` encodes against the current pane protocol, queues input and works while xterm retains control; it does not acquire/release/focus/resize/reconnect. This plan relies on that report and read-only source inspection, not a new live trial.

Local evidence: CLI reports 0.9.0; source archive `/tmp/herdr-socket-source/0.9.0/herdr-b99002ac99b09e00b4ca692436cb15a6b0d676f1`, with adjacent `commit.txt`. Relevant functions: `config/keybinds.rs::parse_key_combo`, `app/api_helpers.rs::encode_api_keys`, `app/api/panes.rs::handle_pane_send_keys`, `pane/terminal.rs::encode_terminal_key_once`, `input/encode.rs`, and vendored Ghostty `input/function_keys.zig` / `key_encode.zig`. Archive paths below `src/` unless marked vendored. Newer/unknown Herdr versions retain existing best-effort treatment.

### Full public inventory: accepted and encoded

| Input | Public names / confirmed encoding |
| --- | --- |
| Common special keys | `enter`/`return`, `esc`/`escape`, `tab`, `backspace`/`bs`, `left`, `up`, `down`, `right`. Each reaches a nonempty special-key encoding. Shift+Tab becomes BackTab; `backtab` itself is not a public name. |
| Function keys | **F1–F12**, with every supported modifier subset. Both Ghostty mapping and Rust fallback stop at F12. F0 and F13–F255 parse but have no function-key encoding; Alt may leave a lone ESC prefix, not a working higher F key. Reject them before RPC; F256+ does not parse. |
| Printable Character | One Unicode scalar accepted by the parser; not ASCII-only. ASCII uppercase implies Shift; other character case is preserved. Multi-scalar graphemes are not one public base: do not truncate or split them into sends. Herdr trims whitespace; expose ordinary space through `space`. |
| Character aliases | `space`, `minus`, `comma`, `period`, `slash`, `backslash`, `quote`, `double_quote`/`double-quote`, `semicolon`, `colon`, `percent`, `ampersand`, `backtick`, `plus`. Literal `+` is normalized only as the entire API key; use `plus` when modified. These are characters, not extra OS keys. |
| Additional API alias | `C-c` / `c-c` means `ctrl+c`; not a general `C-` key syntax. Other name/modifier tokens are case-insensitive. Aliases need no duplicate picker buttons. |
| Rejected special names | Home, End, PageUp/PageDown, Insert/Delete and variants, lock/media/OS keys: absent from the public parser even where internal encoder codes exist. No UI placeholders. |

**Five modifier bits; all 32 subsets with one confirmed base** produce nonempty input through the inspected paths, sometimes as natural aliases. Repeated/alternate spellings do not create extra modifiers. There is no separate modifier-only send or public key-up/hold operation.

| Modifier names | Actual encoding distinction / alias |
| --- | --- |
| `ctrl` / `control`, `shift`, `alt` / `option` / `meta` | Ctrl/Shift/Alt reach the encoder. Legacy characters use control bytes, shifted text or ESC prefixes; special keys have conventional/mode-dependent encodings. Ctrl+Shift letters can alias Ctrl. |
| `super` / `cmd` / `command` | One Super bit. Ghostty encodes it on supported special keys; Rust character encoding carries it in Kitty (e.g. Super+a: `ESC [97;9u`), but legacy characters can alias plain input. This does not invoke universal OS Command behavior. |
| `hyper` | Real Kitty character bit (Hyper+a: `ESC [97;17u`), including combinations. Legacy characters alias; `ghostty_mods_from_key_modifiers` omits Hyper for special keys, so those alias the same key without Hyper in the normal path. Do not claim a distinct Hyper special-key signal. |
| Separate Meta | **Not exposed by this public parser**: `meta` means Alt, not Kitty's separate Meta bit. Option is also Alt. |

These are source-derived capabilities, not a live 32-combination test. Nonempty does not mean distinct or guarantee application action. Natural aliases remain usable. The remaining limitations versus internal Herdr facilities are public-interface limits, not omitted supported picker keys: higher F keys, additional named specials, separate Meta, and multi-scalar bases cannot be supplied faithfully. No new human scope decision is needed to include Super/Hyper under the full-capability approval.

Backspace source evidence (default Ghostty legacy, backarrow/modifyOtherKeys off; `ESC` = `1b`):

| Modifiers | Default legacy bytes | Kitty disambiguation example |
| --- | --- | --- |
| None | `7f` | Normally `7f` |
| Shift | `7f` | `ESC [127;2u` |
| Alt | `ESC 7f` | `ESC [127;3u` |
| Alt+Shift | `ESC 7f` | `ESC [127;4u` |
| Ctrl | `08` | `ESC [127;5u` |
| Ctrl+Shift | `08` | `ESC [127;6u` |
| Ctrl+Alt | `ESC 08` | `ESC [127;7u` |
| Ctrl+Alt+Shift | `7f` (default table fallback) | `ESC [127;8u` |

Rust legacy fallback differs for Ctrl combinations; negotiated modes can also change these results. **Send logical combinations and let Herdr decide.** Natural aliases stay usable; do not promise delete-word behavior or implement these tables as a custom encoder.

## One worker and combined acceptance

Worker **send-keys-worker**, independent reviewer **keys-banner-reviewer**, integrator **keys-banner-integrator**. Main records the governing-plan commit and dispatches the one worker from it. Worker owns key UI/preferences, protocol/client/server wiring, focused checks and deterministic `web/dist`; no dependency upgrade or unrelated change. Reviewer verifies the exact worker commit and server control/security checks independently; integrator accepts only approved results and reports exact integrated SHA/binary. Integration is not human acceptance.

Banner `0f32916521f683c5f6bb6e78f314a665594f25f8` is independently approved, **not deployed**. Keep its history paragraph/behavior out of this edit; coordinate TerminalPage/style overlap. Deliver banner and Send keys in **one combined binary with one authorized stop/start**, after approval.

Focused future production-path gate, real Herdr and dedicated disposable targets only:

- All old keys reachable; explicit Tab/Backspace; representative modified arrows, F1/F12, printable Unicode/space/plus, Ctrl/Super/Hyper combinations, and all Backspace combinations across ordinary/enhanced protocol where available; reject F13/parser-only keys and empty bases while retaining valid Ctrl+Space. Controller retained, one key sent, no appended Enter or Message draft changes.
- Server refusals for forged observer/released/stale-target/cancelled-authority sends; known takeover, disconnect, unknown outcome, and no replay. Interleave text/files/key/Release to verify local coordination and report cross-path limits honestly.
- Real Android keyboard/composition, safe areas/rotation, 44px targets, sheet focus/dismissal, saved edit/remove/reorder/restore/reload, many shortcuts and storage failure. Zero input on select/pin/edit. Message/files, native typing/paste and desktop lifecycle remain intact; check combined banner layout.
- Future automation uses the repository's capped browser entry (`npm --prefix web test`, 512 MiB), primitive assertions, targeted Go checks, typecheck and deterministic build. Real phone acceptance gates downstream work; no sprawling mock QA.

This planning delivery changes and commits only this brief and `ui-direction.md`: no tests, builds, services, terminal input, product edits, merge or push. Implementation begins only when main dispatches the worker.
