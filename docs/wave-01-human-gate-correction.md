# Wave 01 human-gate correction

Status: superseded

This pre-R&D correction brief no longer governs terminal behavior, architecture, interface copy, or acceptance. Direct human decisions made during terminal R&D superseded its persistent mobile ownership model and the related recovery, selection, layout, palette, and wireframe requirements.

Current approved behavior is recorded in `terminal-direction.md` and integrated at `997edbc8ed720d9d7eb88ac3089bd741193de7bd`. In particular, mobile Reader observes between sends, obtains ordinary control only long enough to forward one text or shortcut batch and receive its matching acknowledgement, then releases immediately. If occupied, it sends nothing and offers only a confirmed **Take over and send** retry. Persistent **Control**, **Take over**, and **Release** controls are desktop-only.

The implementation and independent code review are complete. Real-phone product acceptance remains pending; tests and emulator checks do not replace that gate.
