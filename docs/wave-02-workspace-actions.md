# Wave 02: workspace actions

Status: approved

Prepared against `master` at `57b4a084de9bb85f97e865ea73ba300ed22593eb`. Both workers start from the exact commit containing this approved brief; the orchestrator records that commit in both dispatches.

## Outcome

Let one operator manage real Herdr spaces from Home:

- create a top-level space;
- create a linked worktree from a top-level repository group;
- close a workspace;
- close an entire top-level repository group; and
- delete a linked worktree checkout.

Shepherdr uses only Herdr's confirmed operations and changes Home only after Herdr confirms the result. It does not run its own Git workflow or claim success early.

## Confirmed Herdr operations

Use the opaque workspace ID from a fresh Herdr snapshot for every target:

- `workspace.create` with a working directory, optional label, and no focus;
- `worktree.create` with either a top-level workspace ID or an ordinary workspace's server-derived current directory, optional branch, and no focus;
- `workspace.close` with a workspace ID; and
- `worktree.remove` with a workspace ID and `force: false`.

Names and paths are display text and confirmation checks, never mutation targets.

Closing a workspace ends its processes. Closing a top-level workspace closes its whole Herdr repository group. Closing does not delete linked checkouts or branches.

Removing a linked worktree closes its workspace and processes, deletes the checkout, and leaves its branch. Herdr refuses a dirty checkout when force is off. Shepherdr must show that refusal and leave everything intact.

## Actions and placement

Keep the accepted Home layout. A workspace row places its terminal action first and a vertical three-dot menu immediately to its right.

- A top-level repository row offers **New worktree** and **Close workspace**.
- A linked-worktree row offers **Close workspace** and **Delete checkout**.
- An ordinary workspace row offers **New worktree** when its current directory can be resolved truthfully, plus **Close workspace** when Herdr can close it.
- The Home tools row offers the global **New space** action beside Filter and Expand/Collapse. It may wrap on a narrow phone.

Do not add disabled or speculative actions.

## New space

The working-directory field:

- starts with `~`;
- suggests deduplicated checkout paths from currently open top-level workspaces;
- filters those suggestions while the person types; and
- accepts the exact typed value even when it is not a suggestion.

The label is optional. Omit it when blank.

Herdr's socket operation does not expand `~`. In Go, expand only `~` and `~/...` to the current user's home before sending JSON to Herdr. Do not evaluate a shell, environment variables, substitutions, or globs. Send every other value as entered. If the home directory is unavailable, refuse locally in plain language.

Do not add a directory browser, configured roots, saved paths, or extra path validation.

**New worktree** may accept an optional branch. Omit a blank branch and let Herdr choose its normal default.

### Worktree from an ordinary workspace

Herdr does not expose whether an ordinary workspace's current directory is a valid Git source. Shepherdr therefore shows **New worktree** whenever it can resolve that workspace's current directory, and lets Herdr accept or refuse the request. Show a refusal plainly and leave Home unchanged.

The browser still sends only the opaque workspace ID and optional branch. After acquiring the mutation slot, the server reads fresh Herdr state and chooses the source:

- a confirmed top-level repository uses its workspace ID;
- an ordinary one-pane workspace uses that pane's absolute `cwd`;
- an ordinary multi-pane workspace uses its active tab, that tab's focused pane, and that pane's absolute `cwd`.

Never use a browser-supplied directory, `foreground_cwd`, pane order, the first pane, the globally focused pane, a title, or display text. If the current pane cannot be resolved uniquely or its directory is missing or not absolute, omit **New worktree** and refuse a stale submission without calling Herdr.

Changing pane focus or directory before submission is not a destructive stale-confirmation error. The fresh snapshot wins. Herdr decides whether that directory is a valid repository and returns the truthful result.

## Confirmation and interruption

Every close and delete action requires confirmation based on a fresh Herdr snapshot.

A close confirmation names the exact workspace. The menu action is always **Close workspace**, including for a top-level repository.

If other linked workspaces will also close, say how many. Mention affected agents only when the total is greater than zero. Do not show a group warning or a zero-agent sentence when no linked workspace or agent is affected.

When any affected agent is `working`, `blocked`, or `unknown`, the same dialog prominently says those agents may be interrupted and gives the exact nonzero status counts. `idle` and `done` agents remain in a nonzero total but do not trigger the interruption warning. Use one clear dialog, not a second warning.

A delete confirmation names the exact workspace and checkout path and says that the checkout will be deleted while the Git branch remains.

If the target, checkout path, group membership, total agents, or interruption counts change before submission, refuse the stale confirmation and require a fresh one.

## Truthful action flow

1. Opening a destructive action asks the Go server to prepare it from a fresh Herdr snapshot.
2. The browser shows only the returned current scope and path.
3. Confirmation returns the workspace ID and displayed facts as stale-state checks.
4. The server admits one mutation at a time, reads Herdr again, and refuses changed or missing targets without mutating anything.
5. The server sends the exact Herdr operation and waits for its response.
6. A confirmed result requests one complete Home refresh through the existing simple Home path.

Do not optimistically remove, add, or relabel Home rows. Do not queue mutations, poll for completion, or create an action history.

If the phone disconnects after submission, the server finishes waiting for Herdr and releases the action slot. A timeout, socket loss, or Shepherdr restart leaves the outcome unknown: do not retry or claim success. The next connection reads a fresh complete Home.

Render Herdr errors as inert text. Herdr content, names, labels, paths, and error text cannot become markup, commands, routes, or application authority.

## Browser and server contract

Use same-origin JSON routes. Require JSON requests, reject unknown fields, and do not allow cross-origin access.

Each Home workspace adds an `actions` list containing only the actions the Go server confirms are available: `create_worktree`, `close_workspace`, `close_group`, or `delete_checkout`. A top-level workspace may also include its `checkout_path` for New space suggestions. The browser does not infer actions from names, paths, grouping, or IDs.

The shared field names and shapes are:

```text
ConfirmationFacts {
  workspace_label
  scope_workspace_ids[]
  agent_total
  interruption_counts { working, blocked, unknown }
  checkout_path?
}

Prepared {
  outcome: "prepared"
  action
  workspace_id
  expected: ConfirmationFacts
}
```

`scope_workspace_ids` is sorted. The browser retains it for confirmation but does not display it. Counts are numbers, including zero; the interface displays only nonzero interruption counts.

Use two endpoints:

- `POST /api/workspace-actions/prepare` reads fresh Herdr state for a destructive action. Its request contains the action and opaque workspace ID. Its response contains the same action and ID plus the current label, sorted affected workspace IDs, total agents, nonzero interruption counts, and checkout path when deleting a checkout.
- `POST /api/workspace-actions` runs a creation or a prepared destructive action. Creation requests contain the entered fields. A destructive request returns the complete prepared facts unchanged as stale-state checks.

The prepare request is `{ action, workspace_id }`, where action is `close_workspace`, `close_group`, or `delete_checkout`.

The run request is exactly one of:

```text
{ action: "create_space", working_directory, label? }
{ action: "create_worktree", workspace_id, branch? }
{ action: destructive action, workspace_id, expected: ConfirmationFacts }
```

The server reconstructs the prepared facts after acquiring the single mutation slot and requires exact equality before acting. Only the action and opaque workspace ID select the Herdr operation. Labels, paths, counts, and affected IDs are checks, never authority. `force: false` is owned by the server and cannot be supplied by the browser.

For `create_worktree`, the opaque workspace ID selects the fresh Herdr workspace. The server then derives either its confirmed repository workspace ID or its current absolute directory as described above. The browser cannot choose or override that directory.

Responses use one of these outcomes:

- `succeeded` after a matching Herdr success;
- `refused` with a short reason for invalid, missing, inapplicable, busy, stale, unavailable, dirty-checkout, or other Herdr refusal; or
- `unknown` when a mutation may have been sent but no trustworthy matching result arrived.

Optional Herdr error detail remains inert text. The browser never retries an unknown result automatically.

A refusal has `{ outcome: "refused", reason, detail? }`. Reasons are `invalid_request`, `not_found`, `not_applicable`, `busy`, `stale`, `herdr_unavailable`, `checkout_has_changes`, or `herdr_refused`. Unknown is only `{ outcome: "unknown" }`; success is only `{ outcome: "succeeded" }`.

Use HTTP 200 for prepared or succeeded, 400 for an invalid request, 404 for a missing target, 409 for busy, stale, or no longer applicable, 422 for a matching Herdr refusal, and 503 when failure is known to occur before mutation submission. Use 502 or 504 with `unknown` only when a mutation may have been sent but no trustworthy matching response arrived.

After success, the server asks the existing Home reader for one complete refresh. The action response contains no Home patch or created resource. The browser keeps the last complete Home unchanged until the next complete Home update arrives.

## Parallel worker ownership

Both workers start from the same pinned commit containing this brief and contract.

The Go worker exclusively owns:

- Go Herdr action types and calls;
- server-derived Home `actions` and `checkout_path` fields;
- the small in-memory single-action coordinator and action routes;
- the existing Home reader's coalesced refresh request; and
- focused Go tests.

It does not change `web/` or any Terminal contract, bridge, lifecycle, rendering, or test file.

The Home worker exclusively owns:

- strict parsing for the additive Home fields and action responses;
- a small action-request module;
- Home menus, forms, confirmations, feedback, and Home-owned styles;
- focused memory-capped Home tests; and
- concise operator documentation if the finished behavior requires it.

It does not change Go, dependencies, generated browser output, approved documents, or Terminal files and tests.

There is no worker-owned file overlap. Their only seam is the two JSON endpoints, the additive Home fields, and the next complete Home update.

Keep one Go process with the embedded TypeScript browser build. Leave Terminal contracts, rendering, lifecycle, and tests untouched.

The approved ordinary-workspace correction uses one focused Go worker from the integrated Wave 02 candidate. No production browser change is expected because the existing action list, menu, form, and refusal handling already cover it. The worker may add one focused capped browser regression only if needed and must not otherwise change `web/`.

## Tests and evidence

Use a few focused checks for:

- exact opaque workspace targeting and fresh stale-state checks;
- `force: false` on every removal;
- tilde-only expansion and inert free-form paths;
- optional labels and branches;
- repository-parent and ordinary-workspace worktree requests using exactly one server-derived source;
- one-pane and active-tab/focused-pane current-directory resolution, including missing or ambiguous state;
- one mutation at a time;
- Herdr refusal, timeout, disconnect, and unknown outcomes;
- accurate close scope and interruption counts;
- no optimistic Home state; and
- keyboard, touch, focus, scroll, grouping, filtering, and attention preservation.

Browser assertions compare primitive values, never live DOM objects. Run browser tests only through the repository's memory-capped entry. Do not build a large fake Herdr or run broad Terminal tests.

## Review and integration

Each exact worker commit receives independent review against its ownership and the shared contract. Reviewers check target safety, fresh confirmation, non-force removal, truthful results, simple code, inert untrusted text, accepted menu placement, preservation of Home behavior, and no Terminal changes. They may run only focused checks and do not redesign the approved interface.

Only after both results are approved, an integrator starts from this brief's pinned commit, integrates both exact commits, resolves contract mismatches without changing product behavior, runs the focused production gate, and reports the exact integrated commit. Integration is not human acceptance.

## Real-phone gate

Use disposable real workspaces and worktrees through the production path:

1. Create a top-level space using the `~` default, a filtered suggestion, a freely typed path, an optional label, and a real invalid path.
2. Create linked worktrees with an explicit branch and with Herdr's default; Home changes only after confirmation.
3. Create an ordinary space at `~`, change its terminal into a disposable Git repository, and create a worktree from that space. An ordinary non-Git space shows the same action and displays Herdr's refusal without changing Home.
4. Confirm that a working or blocked agent produces the interruption warning with exact counts.
5. Close one linked workspace; its processes end while its checkout and branch remain.
6. Confirm and close one disposable top-level repository. Its menu says **Close workspace**; the dialog names any additional linked workspaces and affected agents, omitting zero counts.
7. Dirty a disposable linked checkout and attempt deletion. Herdr refuses it and nothing disappears.
8. Clean that checkout outside Shepherdr, retry, and verify that the checkout and workspace disappear while the branch remains.
9. Submit a stale confirmation and concurrent actions from two phone tabs; neither may target the wrong workspace or invent success.
10. Recheck grouping, filtering, attention, expansion, scroll, focus, return place, and the existing terminal destination.

Human acceptance gates the next wave.

## Excluded

No force deletion, branch deletion, worktree rename or move, directory browser, configured roots, saved paths, custom Git cleanup or rollback, action queue or history, durable store, optimistic state, polling, authentication, notifications, Terminal changes, or public-hosting work.
