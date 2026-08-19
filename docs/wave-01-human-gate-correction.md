# Wave 01 human-gate correction

Status: **approved for implementation; Wave 01 and product acceptance pending the corrected real-phone gate**

Correction base: `0fcaa39538189fb07a701a9fa9058e704fbefe36`

Authority: the recorded human decisions and final independent technical and language/mobile/visual approvals. This brief is approved for implementation. Wave 01 and product acceptance remain pending the corrected real-phone gate.

## Outcome and diagnosis

Wave 01 remains unaccepted. The phone gate found four blockers:

- Terminal remained **Offline** after a 10-second Tailscale interruption and even after browser refresh, while Home again received live counts.
- A stale `herdr terminal session` child may remain.
- Long-press focuses terminal input/paste instead of selecting and copying output.
- Returning from Terminal loses the previous Home row and scroll position.

The integrated model exposes the seams. Browser `online` currently vetoes terminal attempts and actions even though it is only a hint. Conversely, a locally `OPEN` WebSocket may be a dead path. Terminal children are cancelled without an everywhere-explicit collected exit. Touch is reserved for terminal input, and place restoration is covered only below the real route/layout boundary.

The human also rejected the current visual result: the paper/rust Shepherdr shell is present, but its near-black, mostly black/white PTY reads as a generic separate tool; connection loss is too quiet. Directional keys, an obvious Enter, clearer post-release reacquire, and non-repetitive TalkBack output belong in the immediate terminal UX correction, though the keys are not reconnect root causes.

## Smallest truthful model

Keep three independent facts:

1. **Shepherdr reachability.** Only a successful handshake or traffic confirmed across the path within the stale-detection window is positive evidence; a local write is not enough. Local WebSocket state alone is not. Browser `online`/`offline` events may trigger a probe or reconnect, but never veto an attempt, disable a newer proven connection, or establish that a path is live.
2. **Herdr target truth.** Fresh coherent Home resolution still yields live target, **Herdr is not running**, **Cannot use this Herdr**, or **Terminal unavailable** / “This terminal is no longer here.” Pane route and terminal-freshness rules do not change. A same-generation terminal-ID replacement remains unavailable until explicit reopen from fresh Home; retry cannot bypass it.
3. **This terminal attachment.** Connecting, observing, controlled elsewhere, controlled here, releasing, reconnecting, or retry exhausted. These are connection/control presentation, not agent status.

A terminal stream with no current traffic evidence must become suspect within a finite, tested bound even if the browser still calls it open. No stale-stream detection mechanism is selected here. Once suspected or lost, the old stream cannot validate itself merely by remaining locally open. Replacement requires fresh target resolution, a new Herdr attachment, and its fresh full frame; old and new frame sequences never splice. The old frame may remain visible only with **Reconnecting to terminal** and the human-approved exact sentence **State below may be stale.** Automatic recovery stops 30 seconds after stale/lost detection unless a fresh full frame resets the clock. The exhausted state is **Couldn't reconnect**, with **Try again** and **Home**.

**Offline** applies only when Shepherdr cannot currently be reached and the evidence supports device/network loss. A live Home connection plus failed terminal attachment is not Offline.

Ownership remains observer-first and is independent from selection/input interaction:

- Opening observes, then acquires only free control without takeover.
- Occupied remains **Controlled elsewhere · observing**.
- **Take over** remains explicit and confirms: “Take control? The current controller will lose input.”
- **Release control** leaves the current observer alive and changes the existing ownership action to **Control**. Only that action starts a new non-takeover acquire; terminal taps never acquire. If another controller won, Shepherdr stays observing.

There are exactly two interaction modes—input and Select text—and ownership is a separate axis:

- Without control, Select text behavior is natural and needs no third mode label: touch and TalkBack can partially select and deliberately copy, while input, paste, keys, and surface acquisition are impossible.
- With control, input is the default. **Select text** switches interaction while retaining ownership; selection wins every gesture and suspends all terminal input, paste, and key controls until **Done** returns to input.

Only actual agents expose `working`, `blocked`, `idle`, `done`, or `unknown`. No reconnect or ordinary-terminal status is invented.

## Correction scope

### Gate blockers

1. **Reachability and replacement.** Separate recent traffic evidence, browser hints, Home truth, and terminal attachment. Detect a stale locally-open stream within a documented bound. Retry from a new attachment/full frame. Browser refresh is a gate exercise, not an in-app control.
2. **Exact-child lifecycle.** One server-side owner tracks every exact observe/control child for an attachment. On socket loss, attachment replacement, failed attach, Home navigation, server shutdown, or control release: best-effort release as applicable, cancel the exact child, wait for exit, and use a bounded exact-child kill-and-wait fallback. Every closed/replaced child must reach a collected exit. A generation fence may suppress stale output while cleanup runs; it never counts as exit and never permits overlapping controllers. If bounded cleanup fails, show the short local-failure band **Connection failed**, do not claim recovery, and start no replacement control. Cleanup may continue; a fresh attachment waits for safe collected exit. The phone never exposes child/socket language.
3. **Partial selection and copy.** Implement the two interaction modes above on the gate phone, including with TalkBack. Observers select naturally. Controllers use **Select text** and retain ownership until released separately. Selection never focuses input, pastes, sends bytes, acquires control, or creates history. **Copy** is deliberate; partial selection is required.
4. **Home place.** Save Home mode, pane row, viewport anchor/scroll, and whether list focus existed. After Home or browser Back, restore the same surviving keyed row and visual position after layout settles, without a top flash. If it disappeared, preserve the closest honest position and never retarget another terminal.

### Immediate terminal UX in the same correction

- Trial the Slate PTY palette specified below. Keep the existing paper/rust product shell; correct the generic black PTY with a deliberate working surface and separable ANSI colors. Later color iteration may follow, but no theme or preference system is added.
- Use one prominent state band, not a second banner plus the current tiny subtitle. It is the sole live region and announces meaningful transitions once.
- With control, put one ownership action, **Ctrl C**, and **Enter** first at 390px. **Select text**, **Up**, **Down**, **Left**, and **Right** follow so they do not cover output or push Enter off the initial view. Without control, show the applicable ownership action and selection/copy only—no input keys. All targets remain at least 44px.
- Deduplicate accessible names without dropping visible information: Home rows retain approved **Open …** names and secondary/status text; the Terminal heading is the human title once; the surface is **Terminal**; the state band is the only `role="status"`; controls use short names without workspace repetition.
- Rotation and the Android keyboard refit to the actual visible viewport and safe areas without changing pane, attachment, ownership, or interaction mode. With input active, the terminal cursor/current input line and first-row ownership, Ctrl C, and Enter remain above the keyboard. Geometry changes are coalesced so one stable change produces at most one applicable terminal resize and no duplicate input. Selection and the last frame survive the refit. Closing the keyboard restores the terminal without a blank frame, jump, overlap, or lost place. The behavior follows measured visible geometry whether a browser overlays or resizes layout; no framework is selected here.

One continuing worker owns these coupled seams and implements the correction on top of `0fcaa39538189fb07a701a9fa9058e704fbefe36` from the exact future dispatch base that includes this brief. The orchestrator records that document commit in dispatch; this brief does not invent it. Independent technical review, independent language/mobile/accessibility review, integration, and the human-witnessed phone gate remain separate. Existing exact-once Home, blocked-filter, disambiguation, route, hostile-content, observer/takeover, and `50990e5` no-flicker requirements remain in force.

No Chat, auth/device work, notifications, durable terminal history, second runtime, multiple configured sessions, new Herdr API, or grouping/nesting/collapse enters this correction.

## 390px phone wireframes

Controlled chrome is one sticky ownership slot—**Release control**—then **Ctrl C**, **Enter**, and overflow interaction/keys. At 390px those first three remain visible. Without control, **Control** or **Take over** remains the sole ownership action beside selection/copy. Controlled selection keeps ownership but replaces input keys with Copy and Done.

```text
NORMAL 390px ROW REFERENCE
[Release control] [Ctrl C] [Enter]  ›
[Take over]       selection available
[Control]         selection available
```

```text
RECONNECTING
┌──────────────────────────────────────┐
│ [Home]  Human terminal title         │
├──────────────────────────────────────┤
│ Reconnecting to terminal             │  one state band
│ State below may be stale.            │
├──────────────────────────────────────┤
│                                      │
│ last rendered terminal frame         │
│                                      │
└──────────────────────────────────────┘
```

```text
RETRY EXHAUSTED — AFTER 30 SECONDS
┌──────────────────────────────────────┐
│ [Home]  Human terminal title         │
├──────────────────────────────────────┤
│ Couldn't reconnect                   │
│ State below may be stale.            │
│ [Try again]  [Home]                  │
├──────────────────────────────────────┤
│ last rendered terminal frame         │
└──────────────────────────────────────┘
```

```text
SELECTING WHILE RETAINING CONTROL
┌──────────────────────────────────────┐
│ [Home]  Human terminal title         │
├──────────────────────────────────────┤
│ Select text                          │  mode/state, not agent status
├──────────────────────────────────────┤
│ terminal frame with partial          │
│ selection                            │
├──────────────────────────────────────┤
│ [Release control] [Copy] [Done]      │  input/keys suspended
└──────────────────────────────────────┘
```

```text
AFTER RELEASE — SELECTION AVAILABLE
┌──────────────────────────────────────┐
│ [Home]  Human terminal title         │
├──────────────────────────────────────┤
│ Observing · input is unavailable     │
├──────────────────────────────────────┤
│ terminal frame; touch/TalkBack       │
│ may make a partial selection         │
├──────────────────────────────────────┤
│ [Control]  [Copy when selected]      │  surface tap never acquires
└──────────────────────────────────────┘
```

```text
ANDROID KEYBOARD OPEN — VISIBLE VIEWPORT
┌──────────────────────────────────────┐
│ [Home]  Human terminal title         │
│ terminal … active input line █       │
│ [Release control] [Ctrl C] [Enter] › │
├──────────────────────────────────────┤
│ Android keyboard (outside app area)  │
└──────────────────────────────────────┘
```

## Approved decisions

The four human decisions are fixed implementation inputs:

- stop automatic recovery after 30 seconds; show **Couldn't reconnect**, **Try again**, and **Home**;
- trial Slate now, allowing later color iteration without a theme/preference system;
- keep ownership separate from the two interaction modes: observers naturally view/select; controllers default to input and deliberately enter **Select text** until **Done**;
- after release, reuse the ownership slot as **Control** for deliberate non-takeover reacquire; never acquire from a terminal-surface gesture.

Slate keeps shell paper `#f5f1e8`, charcoal text `#292925`, and one rust primary/focus `#b85c32`. The PTY trial uses background `#18201e`, warm text `#f3eadb`, selection `#355b63`, and ANSI base `#2b3532`, `#ef7f80`, `#91c98f`, `#d8bd6a`, `#82afe3`, `#c999d0`, `#77c5bf`, `#e6ded1`; bright variants remain distinct and contrast-validated. Rust is not ANSI red, and the PTY is neither pure black/white nor beige.

## Architecture and trust boundaries

Use only confirmed Herdr 0.8.0 capabilities already approved: fresh snapshot resolution; `terminal session observe` full/delta ANSI frames; `terminal session control`; input, resize, scroll, release, and takeover. There is no replay cursor, terminal history, controller query, or new Herdr interface.

The browser owns presentation, the fixed 30-second recovery policy, selection/input interaction, place, current viewport geometry, and its current transport evidence. It responds to measured visible geometry rather than assuming whether the Android keyboard overlays or resizes the layout; no UI framework is selected here. The Shepherdr server owns exact route resolution and collection of its exact Herdr client children. Herdr owns the PTY/process, frames, and control truth. Stale frames, browser hints, names, metadata, ANSI bytes, selected text, and pasted text are untrusted. Names and metadata may appear as inert escaped visible/accessibility label text; they cannot define label structure, markup, route syntax or retargeting, application actions, authorization, control messages, implicit input, or clipboard writes. Clipboard writing requires explicit Copy.

## Corrected acceptance gate

Run the production build with a real compatible Herdr server, the same real phone/browser, and Tailscale. Fixtures may force hostile or cleanup-failure edges, but do not replace the real workflow.

1. **Baseline:** Home still shows every pane exactly once, including both `w2S` terminals and ordinary terminals. Agent status/attention, blocked reuse, collision titles, pane routes, and stable no-flicker publication remain correct.
2. **Reachability evidence:** instrument handshake/traffic time, browser hint changes, stale-stream suspicion, attachment generation, and full-frame arrival without exposing these words in UI. Prove a locally-open but dead socket becomes suspect within the documented bound; `online` cannot keep it falsely live. Prove a false `offline` hint cannot block a new attempt, control/input backed by current traffic, or override newer successful traffic. Neither local `OPEN`, a buffered local write, nor `online` alone is success.
3. **Real 10-second failure:** exchange a unique marker, interrupt Tailscale for 10 seconds, restore it, and verify the exact stale sentence, fresh attachment/full frame, and a second marker exactly once. Terminal must not stay Offline while current traffic proves Shepherdr reachable. Repeat using the phone browser's Refresh on the pane route; no Refresh control is added to Shepherdr.
4. **Recovery policy:** exercise replacement attempts through 30 seconds from stale/lost detection. A fresh full frame alone resets the clock. At exhaustion verify **Couldn't reconnect**, **Try again**, and **Home**, with no continuing automatic loop; **Try again** creates one fresh attachment attempt and a new 30-second window. Not-running, incompatible, Terminal unavailable, and local attachment-cleanup failure remain distinct.
5. **Collected child exit:** inventory exact observe/control children. Exercise disconnect, automatic and manual retry replacement/exhaustion, refresh, navigation Home, failed attach, release/reacquire, occupied/takeover, rotation, Shepherdr server shutdown, and Herdr stop/restart. Every child from a closed/replaced attachment reaches collected exit; no old frame or controller survives. After Release, exactly the current attachment's live observer may remain while its old controller is collected. Force bounded cleanup failure in a supporting harness: stale output is fenced, **Connection failed** is truthful, and no replacement controller starts.
6. **Selection/copy:** while observing without control, partially select and copy known visible plain and ANSI-rendered text with touch and TalkBack; surface gestures never acquire. While controlled, confirm input is the default, choose **Select text**, retain ownership, and partially select/copy; all input, paste, and key controls remain suspended until **Done**. Copy is deliberate and the result matches visible text. No selection action creates input, acquire, takeover, or history.
7. **Ownership:** verify free acquire, occupied observer, dismissed/confirmed takeover with unchanged copy, release, **Control** in the existing ownership slot, deliberate non-takeover reacquire, and the race where another controller wins. Tapping the surface never acquires. Selection always wins gesture arbitration; no first key disappears or sends before confirmed control.
8. **Place:** below the fold, return by Home and browser Back to the same surviving row and visual position without a top flash. Repeat from Blocked/Show all, after a Home update, and across rotation. A missing row never retargets.
9. **Pixel 8a viewport, TalkBack, rotation, and visuals:** on a real Pixel 8a, test portrait and landscape. In controlled input with the Android keyboard open, keep the active cursor/input line and first-row Release control + Ctrl C + Enter above it. Enter **Select text** while the keyboard is present: ownership remains, input stops, partial selection survives, and Release control + Copy + Done remain usable whether the keyboard closes or continues to overlay. **Done** returns to input cleanly. Record whether the real browser overlays or resizes; if it cannot expose both, a supporting browser geometry test covers the other behavior without replacing the Pixel gate. For each settled geometry, send at most one applicable resize and no duplicate input; pane, attachment, ownership, and selected text remain fixed. Keyboard close restores frame/place without blanking, jump, or overlap. At 390px, Select text and arrows remain after Enter; actions are 44px; the state band is the only live region; labels do not repeat or hide visible status. Refresh screenshots for normal controlled input, keyboard-open input, keyboard transition into/out of selection, occupied natural selection, released **Control**, controlled selection, reconnecting, 30-second exhaustion, Home-place before/after, portrait, and landscape. Validate the Slate trial, ANSI distinctions, cursor/selection, and 4.5:1 text contrast.
10. **Hostile content:** hostile names/metadata appear only as inert escaped label text and terminal bytes. They cannot create label structure, markup, route syntax/retargeting, actions, authorization, control/input, or clipboard writes. ANSI/control sequences cannot escape the terminal renderer.

This brief is approved for implementation only. Wave 01 and product acceptance remain pending independent review of the exact implementation, integration evidence, and human acceptance of the complete corrected real-phone gate.

## Risks and limits

- The exact phone event sequence still needs instrumentation; the observed failure is authoritative, but local socket state or browser hints cannot be assumed causal by themselves.
- Herdr supplies a fresh current frame, not offline replay. Lost historical output cannot be promised.
- Touch and TalkBack selection vary by browser/renderer and must pass on the gate phone.
- Android keyboards may resize or overlay browser layout. Acceptance follows the measured visible viewport and observed result, not user-agent assumptions; refit must not manufacture a new attachment or repeated resize/input.
- Cleanup targets only Shepherdr's exact children, never the Herdr server or Herdr-owned terminal process.
- Exact place restoration assumes the row survives; topology changes never justify label retargeting.
