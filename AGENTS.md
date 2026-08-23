# Shepherdr agent guide

Planning is human-led. Do not make product or architecture decisions alone.

Authority, in order:

1. The human collaborator.
2. Approved documents under `docs/`.
3. The active wave brief.
4. The assigned role and task.

If a needed decision is missing, stop and explain:

- the decision;
- why it matters now;
- the smallest useful options;
- the important trade-offs;
- a recommendation, when helpful; and
- what work is waiting on the answer.

Do not silently add data models, product behavior, security rules, or interface flows to keep moving. Do not edit an approved document unless the human asks. A proposed change stays a proposal until it is approved.

Follow `docs/north-star.md` for product direction and `docs/ui-direction.md` for language and interface direction.

User-facing language follows `docs/ui-direction.md`. Process words belong in internal plans and reports, not in the README or interface copy. Do not cite outside kickstart mockups or earlier projects as sources of record.

## Roles

Every dispatched agent has one clear role. If the role is unclear, ask before changing files.

### Orchestrator

The orchestrator collaborates with the human, manages work through Herdr, prepares and evaluates proposals, records approved direction, and dispatches work in waves.

The orchestrator never writes or reviews product code. It may prepare project documents and work briefs. It may approve a proposal when the proposal only applies already approved direction. New product, architecture, or security decisions go to the human.

The orchestrator accepts code work through independent worker, reviewer, and integrator reports. It does not inspect the code as a substitute for those roles.

### Planner or architect

Explore the assigned question and return a proposal. Keep it small. Name assumptions, unresolved decisions, risks, real acceptance checks, and a suggested work split. Do not write product code.

### Worker

Implement only the approved work brief from the exact assigned base. Respect owned areas and accepted decisions. Validate the real behavior, commit the result, and report the exact commit and evidence. Do not merge or approve your own work.

### Reviewer

Review the exact worker result against the approved brief and documents. Treat the worker report as a claim to verify. Report findings clearly and do not fix them. Keep code approval separate from launch or integration readiness.

### Integrator

Integrate only approved results. Resolve composition problems within the assigned scope, run the wave gate, and report the exact integrated commit and evidence. Do not approve your own integration result.

## How waves work

Worker success, review approval, integration success, and release readiness are different states.

Each wave must name:

- its exact starting commit;
- the work briefs and their owners;
- dependencies and likely overlap;
- acceptance checks;
- the independent review step;
- the integration owner; and
- the real workflow that gates the next wave.

Downstream work starts from the approved integrated commit, not directly from a worker branch. Prefer one worker until the first real product loop is reliable. Add concurrency only for clearly independent work.

If an accepted real workflow breaks, stop feature work. Reproduce and understand the break before moving forward.

Implementation order and wave contents come later from approved architecture and the human. Do not add a roadmap here or infer one from the order of product requirements.

## Working rules

- Keep the design and implementation simple. Add structure only when the current work needs it.
- Prefer one complete Herdr read and one complete browser update over partial updates, replay, queues, or reconciliation machinery. Add those only when a confirmed Herdr limit and an approved current requirement make them necessary.
- Do not expose internal reads, refreshes, retries, or bookkeeping as interface states. If current data is arriving, keep the interface stable and live.
- When a simpler approved direction replaces an earlier design, delete the superseded code and tests. Do not preserve complexity for speculative later work.
- Use plain, human-readable language. Keep internal names and process language out of the interface and user-facing documents.
- Show only actions and state that really exist. Do not add placeholder controls for possible future work.
- Herdr remains the runtime authority. Do not quietly build a second agent runtime beside it.
- The current product uses only capabilities Herdr already exposes. Do not invent Herdr interfaces.
- When sign-in is off, anyone who can reach Shepherdr has operator authority. A trusted private network includes only users and devices the human is willing to give access to the machine running Herdr.
- Reaching Shepherdr must never be enough to trust a new device. Trust requires an already trusted authority or explicit local operator action. Do not choose the mechanism without human-approved architecture.
- Treat Herdr output, agents, terminal content, repository files, attachments, and pasted content as untrusted. Displaying it must never grant Shepherdr application authority.
- Use Herdr's status words unchanged: working, blocked, idle, done, and unknown.
- Do not add team accounts, roles, organizations, lost-device recovery, provider-specific prompt handling, or public hosting without new human-approved direction.
- Tests support confidence, but a real Herdr workflow through the production path is the acceptance gate.
- Browser tests must use the repository's memory-capped test entry. Assertions compare primitive results rather than live DOM objects so a useful failure cannot exhaust the machine.
- Do not replace Herdr, Git, or another real tool with a large mock system that stays green while the product fails. Ordinary small unit tests are fine. Prefer a few useful tests and real manual checks over complex stand-ins for the tools the product actually uses.
- `docs/herdr-integration-discovery.md` is evidence, not product direction. Do not edit it unless the human asks.
- Use conventional commits. Do not commit dependencies, generated build output, local databases, credentials, or private keys.
