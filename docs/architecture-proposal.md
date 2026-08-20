# Shepherdr Home and workspace-management architecture

Status: approved

Approved reconciliation: 2026-08-20

## Scope and shape

This architecture covers the non-terminal stream: read-only Home first, followed by New worktree, linked-workspace Close, and non-force Remove. Terminal implementation is separate. Sign-in, devices, notifications, and Chat remain later work.

The system has three live parts:

1. One configured Herdr 0.8.0 session remains authoritative for workspaces, tabs, panes, terminals, layouts, agents, focus, and worktree operations.
2. One Shepherdr process binds to `localhost`, talks to that session, serves a same-origin embedded TypeScript UI, and owns the coherent in-memory projection and mutation coordinator.
3. A phone browser renders that UI and sends only typed requests allowed by the server.

There is no durable Home, grouping, or action store. Home is rebuilt from Herdr. The current private-network and access policy remains unchanged.

## Confirmed Herdr 0.8.0 contract

`session.snapshot` provides the canonical workspace, tab, pane, terminal, layout, agent, focus, version, and protocol data. Workspace provenance includes `repo_key`, `checkout_path`, and `is_linked_worktree`.

The scoped `worktree.list({workspace_id})` response provides source provenance and entries containing `path`, `branch`, `open_workspace_id`, `is_bare`, `is_prunable`, and `is_linked_worktree`. Herdr has no group API. Git worktree enumeration is validation evidence, not Home display order.

`events.subscribe` covers the workspace, tab, pane, layout, agent, focus, status, and worktree lifecycle events relevant to the current projection or an outstanding action. Subscriptions are scoped to the configured session and connection generation. Herdr status words remain `working`, `blocked`, `idle`, `done`, and `unknown`.

Only confirmed Herdr 0.8.0 requests and response/event forms are used. The browser transport adds no product semantics, and terminal output never drives Home, agent state, grouping, or action results.

## Fresh coherent projection

Shepherdr establishes all relevant subscriptions before any read may contribute to publication. Reconnaissance reads may discover what needs subscription, but they can never publish. Each attempt builds a fresh candidate from contributing reads taken after subscription coverage exists.

A candidate is coherent only when:

- workspace, tab, pane, and terminal IDs are unique;
- every workspace/tab/pane/terminal reference joins by exact equality and agrees in all identity fields;
- layout identity is unambiguous and every layout reference joins exactly to canonical panes; layouts may order terminals but never create, duplicate, or hide them;
- each agent decoration joins by exact workspace, tab, pane, and terminal equality; and
- each unique valid real agent is counted once.

Any relevant event observed from the first contributing read through publication invalidates the entire candidate and restarts the attempt. Missing, malformed, duplicate, ambiguous, or conflicting identity data also prevents publication. Publication is atomic: the browser receives one complete coherent Home, never a partial or mixed view.

Once published, relevant events and Home resume trigger another covered rebuild. An unchanged semantic projection does not rerender. Connection generation changes, subscription loss, incompatible data, or an invalid join remove fresh authority and trigger reconciliation rather than replay or inference.

## Derived worktree nesting and order

Nesting is derived only from exact nonempty, schema-valid opaque `repo_key` equality in a fresh coherent snapshot. A valid nest has exactly one workspace with `is_linked_worktree:false` and one or more with `is_linked_worktree:true`. A null or malformed key, a linked-only set, or a key with multiple nonlinked workspaces remains flat. Labels, paths, branches, and list order never define grouping.

A nest replaces its nonlinked workspace at that workspace's Herdr top-level position. Its linked children are removed from their former top-level positions and retain their original Herdr relative order inside the nest, including when a child originally appeared before the parent. All unrelated workspaces retain Herdr order. Shepherdr stores no grouping and does not use Git worktree enumeration as display order.

Ordinary flat Home remains compact. An expanded nest contains every workspace and every terminal. Its header uses the nonlinked workspace's display title once, the workspace count, and only nonzero actual agent totals in fixed Herdr order: `working`, `blocked`, `idle`, `done`, `unknown`. Counts are inert.

## Disclosure, Open, Blocked, and visit state

The nest header has two sibling 44-pixel targets: the title/disclosure area and, when valid, the parent's current-terminal `Open`. The parent Open is derived only through this exact chain:

1. `active_tab_id` resolves to exactly one canonical tab in that workspace.
2. Exactly one canonical layout matches that workspace and tab.
3. Its `focused_pane_id` resolves to exactly one canonical pane.
4. The pane's workspace, tab, pane, and terminal identities match exactly throughout the candidate.

Missing or ambiguous data at any step omits parent Open. There is no first-terminal fallback. Terminal Opens also require an exact current terminal. Accessible names use the human terminal title and place; internal identity terms never appear. Terminal chevrons are decorative.

The first loaded visit expands nests containing `working` or `blocked` agents and collapses the rest. After initialization, manual expand/collapse state wins for that visit even when later statuses change. `Expand all` appears when any nest is collapsed; `Collapse all` appears when every nest is expanded. Neither appears without nests or in Blocked view.

Blocked view is a temporary projection of only blocked terminal rows plus the minimum workspace context. It omits nest disclosure, parent Open, nest totals, Expand/Collapse all, New worktree, and Actions. It does not mutate remembered Home state. Returning restores Home scroll, focus, and expansion.

## Connection state and content truth

The existing transport and 45-second offline threshold remain. A stable top-right badge slot shows `Live`, `Reconnecting`, `Offline`, `Herdr is not running`, or `Cannot use this Herdr` without layout shift. Valid frames for the current connection generation mean Live. Internal Home reconciliation never changes a healthy Live badge to Reconnecting. A stopped or incompatible Herdr suppresses Live and does not open the reconnect sheet.

Transport health does not make Home coherent. While a fresh projection is invalid, Shepherdr may retain prior coherent rows with a short updating note only when they are still honest. Otherwise it replaces them with an updating or unavailable state. Every terminal Open, parent Open, and management control is absent until atomic coherent publication. Home resume rebuilds without a healthy-Live flicker.

## Trust and mutation authority

All names, IDs, paths, branches, repository keys, Herdr content, repository content, attachments, and pasted content are inert untrusted data. They may be displayed safely but cannot define grouping, authorization, request kinds, routes, commands, or operation targets.

Before any mutation, the server enforces the existing same-origin and access policy. One server-owned single-flight coordinator for the configured session is scoped to the connection generation. It spans fresh projection and operation-specific validation, typed request construction, confirmation/classification, and coherent Home reconciliation.

The browser may supply only an untrusted target ID and, for creation, an optional branch string. It cannot set force, focus, `cwd`, base, path, label, repository identity, or operation kind. The server derives every authoritative field from fresh Herdr data. Management controls are absent without a fresh coherent projection, while the coordinator is locked, and throughout action reconciliation.

One outstanding mutation excludes all others across browser tabs. A connection-generation change, malformed or mismatched result, or loss of required fresh authority prevents inferred success. A confirmed action followed by a delayed Home rebuild remains confirmed and shows Home updating; it does not become unknown.

## New worktree correlation

New worktree is available later to an eligible flat nonlinked top-level workspace, including before it has a linked child. The server freshly validates the target, omits a blank branch so Herdr chooses, and always sets `focus:false`. It derives all other request fields.

Only the matched `worktree_created` response for that locked request confirms creation. An event alone never confirms it. A missing, interrupted, malformed, or mismatched response is `Result unknown`; there is no automatic retry, fabricated preview, or inferred success.

## Linked-workspace Close correlation

Close is available later only for a freshly validated linked workspace, never for its nonlinked parent or an ordinary flat workspace. The server sends the validated workspace ID.

A matched generic `ok` response confirms close. An exact `workspace_closed` event observed after the action begins and matching the validated workspace may also confirm it. Otherwise the result is unknown. Closing ends terminals but retains the folder and branch.

## Non-force Remove correlation

Remove is available later only after a fresh, same-generation scoped `worktree.list` whose `source.repo_key` exactly matches snapshot provenance and where exactly one entry matches both the validated `open_workspace_id` and `checkout_path`. That entry must have `is_linked_worktree:true`, `is_bare:false`, and `is_prunable:false`.

The server sends the validated workspace ID with `force:false`. It never accepts a browser path, forces removal, or deletes the branch. The UI displays the exact validated path inertly.

An exact matched `worktree_removed` response confirms removal. An exact lifecycle event observed after locked validation and action begin may also confirm only when workspace, path, and `forced:false` all match. Response/event reordering is allowed only through those exact correlations. Missing, interrupted, malformed, mismatched, or lost evidence is unknown. A dirty refusal is not success and leaves the folder and branch intact.

## Sequential slices and ownership

Work proceeds sequentially from the preceding approved integrated commit:

1. read-only Home;
2. New worktree;
3. linked-workspace Close;
4. non-force Remove.

Each slice has one worker, an independent technical reviewer, an independent Grok UX reviewer, and a separate integrator. The slice gate runs the production build, Android Pixel 8a emulator validation where relevant, then a human-run or human-witnessed real-phone workflow. A failed real workflow stops the next slice. Worker success, review approval, integration success, and human acceptance remain separate states.

## Acceptance gates

Read-only Home must demonstrate:

- compact flat density and exact-once display of every workspace and terminal;
- derived nesting for noncontiguous children, children before the parent, and flat treatment of null, malformed, linked-only, or multiple-parent provenance;
- every expanded workspace and terminal, exact unique-agent counts in fixed order, exact current-terminal Open, and omission for missing or ambiguous resolution;
- first-visit defaults, manual-collapse precedence, Expand/Collapse all, focus and scroll restoration, and Blocked filtering with its required omissions;
- a stable badge with no internal-refresh or resume flicker, stopped/incompatible handling, and the 45-second offline threshold; and
- atomic rebuild across resume, relevant events, event/read races, invalid joins, subscription or connection loss, hostile display data, and immediate stale-affordance removal.

New worktree adds named and blank-branch creation, branch omission, `focus:false`, focus preservation, hostile and tampered browser inputs, cross-tab concurrency, connection loss, exact response matching, and unknown-result behavior. Actions remain absent throughout the outstanding request and reconciliation.

Close adds linked-only eligibility, parent and ordinary-workspace exclusion, exact response/event ordering and loss, terminal ending, and verified folder and branch retention.

Remove adds clean removal with branch retention, dirty refusal, exact inert path display, `force:false`, tampered path/force rejection, and exact response/event ordering and loss. It never removes an ineligible, bare, prunable, mismatched, or ambiguously matched checkout.

Tests and fixtures support these cases but do not replace the production Herdr workflow, emulator check where relevant, or real-phone human gate.

## Assumptions and risks

Assumptions:

- The product uses one configured local Herdr 0.8.0 session and its confirmed interfaces.
- Workspace, tab, pane, terminal, layout, agent, focus, and worktree provenance needed for a coherent candidate are available or the Home remains honestly unavailable.
- A modern phone browser supports the same-origin transport through the operator's trusted private-network route.
- Herdr's status and worktree validation are the runtime truth; Shepherdr does not supplement them from Git or terminal content.

Risks:

- Continuous relevant events can repeatedly invalidate a candidate and delay coherent publication. Honest delay is preferred to mixed state.
- Missing or malformed provenance keeps workspaces flat; it must never be guessed from labels, paths, branches, or Git order.
- Connection loss during an action can leave an honestly unknown result. Single-flight and no retry prevent compounding it.
- Delayed or reordered lifecycle evidence can resemble success for another operation unless generation, timing, identity, and request correlation remain exact.
- Mobile browsers and private proxies may interrupt streams; resume always rebuilds current truth rather than replaying an assumed gap.
