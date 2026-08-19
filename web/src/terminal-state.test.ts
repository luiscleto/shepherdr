import assert from "node:assert/strict";
import test from "node:test";

import {
  hasCurrentHomeEvidence,
  hasRecentTraffic,
  protocolOrderingEvidence,
  RecoveryWindow,
  TerminalRecoveryPolicy,
  TerminalSelection,
  TERMINAL_RECOVERY_LIMIT_MS,
  TERMINAL_STALE_AFTER_MS,
  terminalControlLabels,
  terminalStateActionLabels,
  terminalStateCopy,
} from "./terminal-state";

const epochA = "a".repeat(32);
const epochB = "b".repeat(32);

function evidence(epoch: unknown, gap: unknown) {
  return protocolOrderingEvidence({ epoch, gap });
}

test("stale traffic and recovery exhaustion use fixed bounded evidence", () => {
  assert.equal(hasRecentTraffic(10, 10 + TERMINAL_STALE_AFTER_MS - 1), true);
  assert.equal(hasRecentTraffic(10, 10 + TERMINAL_STALE_AFTER_MS), false);
  assert.equal(hasRecentTraffic(undefined, 10), false);
  assert.equal(hasCurrentHomeEvidence(true, true, 10, 10 + TERMINAL_STALE_AFTER_MS - 1), true);
  assert.equal(hasCurrentHomeEvidence(true, true, 10, 10 + TERMINAL_STALE_AFTER_MS), false);
  assert.equal(hasCurrentHomeEvidence(true, true, undefined, 10), false, "OPEN alone is not current Home evidence");
  assert.equal(hasCurrentHomeEvidence(false, true, 10, 10), false);

  const recovery = new RecoveryWindow();
  recovery.markLost(1_000);
  assert.equal(recovery.exhausted(1_000 + TERMINAL_RECOVERY_LIMIT_MS - 1), false);
  assert.equal(recovery.exhausted(1_000 + TERMINAL_RECOVERY_LIMIT_MS), true);
  recovery.markFullFrame();
  assert.equal(recovery.exhausted(100_000), false, "only a fresh full frame resets recovery");
  recovery.manualRetry(200_000);
  assert.equal(recovery.exhausted(200_000 + TERMINAL_RECOVERY_LIMIT_MS), true);
});

test("terminal copy keeps recovery truth prominent and interaction separate from ownership", () => {
  assert.deepEqual(terminalStateCopy("reconnecting", "input", true, false), {
    heading: "Reconnecting to terminal",
    body: "State below may be stale.",
  });
  assert.deepEqual(terminalStateCopy("retry_exhausted", "input", true, false), {
    heading: "Couldn't reconnect",
    body: "State below may be stale.",
  });
  assert.deepEqual(terminalStateCopy("controlled", "select", true, false), {
    heading: "Select text",
    body: "Input is paused.",
  });
  assert.equal(terminalStateCopy("controlled", "input", true, true).heading, "You have control");
  assert.deepEqual(terminalStateCopy("terminal_unavailable", "input", true, false), {
    heading: "Terminal unavailable",
    body: "This terminal is no longer here.",
  });
});

test("one server epoch orders Home recovery and terminal target status in both delivery orders", () => {
  const staleHome = new TerminalRecoveryPolicy();
  assert.equal(staleHome.home("live", evidence(epochA, 4)), false);
  assert.equal(staleHome.status("herdr_not_running", evidence(epochA, 5)), "detach");
  assert.equal(staleHome.automatic, false, "a newer target failure must beat remembered live Home");
  assert.equal(staleHome.home("live", evidence(epochA, 4)), false, "replayed stale Home cannot resume recovery");
  assert.equal(staleHome.home("live", evidence(epochA, 5)), true, "same-generation live recovery is newer projector truth");
  assert.equal(staleHome.automatic, true);

  const liveDeliveredFirst = new TerminalRecoveryPolicy();
  assert.equal(liveDeliveredFirst.home("live", evidence(epochA, 7)), false);
  assert.equal(liveDeliveredFirst.status("incompatible", evidence(epochA, 7)), "recover");
  assert.equal(liveDeliveredFirst.automatic, true, "same-generation live truth wins regardless of socket delivery order");

  const newerFailure = new TerminalRecoveryPolicy();
  newerFailure.home("live", evidence(epochA, 7));
  assert.equal(newerFailure.status("incompatible", evidence(epochA, 8)), "detach");
  assert.equal(newerFailure.automatic, false);

  const unorderedFailure = new TerminalRecoveryPolicy();
  unorderedFailure.home("live", evidence(undefined, 9));
  assert.equal(
    unorderedFailure.status("herdr_not_running", evidence(undefined, 10)),
    "detach",
    "missing epoch evidence must not mask target truth",
  );
  assert.equal(unorderedFailure.home("live", evidence(undefined, 10)), false, "missing epoch evidence stays conservative");

  assert.equal(staleHome.status("terminal_unavailable", evidence(epochA, 5)), "detach");
  assert.equal(staleHome.home("live", evidence(epochA, 6)), false, "confirmed same-generation absence stays unavailable");
});

test("a new server epoch supersedes retained not-running and incompatible target evidence", () => {
  for (const mode of ["herdr_not_running", "incompatible"] as const) {
    const policy = new TerminalRecoveryPolicy();
    assert.equal(policy.home("live", evidence(epochA, 9)), false);
    assert.equal(policy.status(mode, evidence(epochA, 10)), "detach");
    assert.equal(policy.automatic, false);
    assert.equal(policy.home("live", evidence(epochA, 9)), false, "old Home replay cannot recover the target");
    assert.equal(policy.home("reconnecting", evidence(epochB, 0)), false, "new process must first publish live truth");
    assert.equal(policy.home("live", evidence(epochB, 0)), true, `${mode} must yield to current truth from the new process`);
    assert.equal(policy.automatic, true);
  }
});

test("only exact server epoch and gap values become ordering evidence", () => {
  assert.deepEqual(evidence("0123456789abcdef0123456789abcdef", 0), {
    epoch: "0123456789abcdef0123456789abcdef",
    gap: 0,
  });
  assert.deepEqual(evidence("f".repeat(32), Number.MAX_SAFE_INTEGER), {
    epoch: "f".repeat(32),
    gap: Number.MAX_SAFE_INTEGER,
  });

  for (const malformedEpoch of [
    undefined,
    "",
    "bad",
    "g".repeat(32),
    "A".repeat(32),
    "a".repeat(31),
    "a".repeat(33),
    42,
  ]) {
    assert.equal(evidence(malformedEpoch, 0), undefined, `accepted malformed epoch ${String(malformedEpoch)}`);
  }
  for (const malformedGap of [undefined, "0", -1, 0.5, Number.MAX_SAFE_INTEGER + 1, Number.NaN, Number.POSITIVE_INFINITY]) {
    assert.equal(evidence(epochA, malformedGap), undefined, `accepted malformed gap ${String(malformedGap)}`);
  }
});

test("malformed epoch or gap evidence cannot recover retained target failure", () => {
  for (const malformed of [
    { epoch: "bad", gap: 0 },
    { epoch: "g".repeat(32), gap: 0 },
    { epoch: 42, gap: 0 },
    { epoch: epochB, gap: "0" },
    { epoch: epochB, gap: -1 },
    {},
  ]) {
    const policy = new TerminalRecoveryPolicy();
    policy.home("live", evidence(epochA, 9));
    assert.equal(policy.status("herdr_not_running", evidence(epochA, 10)), "detach");
    assert.equal(policy.home("live", protocolOrderingEvidence(malformed)), false);
    assert.equal(policy.automatic, false, `malformed evidence enabled recovery: ${JSON.stringify(malformed)}`);
  }

  const malformedFailure = new TerminalRecoveryPolicy();
  malformedFailure.home("live", evidence(epochA, 9));
  assert.equal(malformedFailure.status("incompatible", evidence(epochA, "10")), "detach");
  assert.equal(malformedFailure.home("live", evidence(epochB, 0)), false);
  assert.equal(malformedFailure.automatic, false, "malformed target order must remain conservative across epochs");
});

test("a terminal frame mutation invalidates selection text, Copy authority, and held geometry", () => {
  const selection = new TerminalSelection();
  assert.equal(selection.update("partial output"), false);
  assert.equal(selection.copyAvailable, true);
  assert.equal(selection.text, "partial output");
  assert.equal(selection.invalidate(), true);
  assert.equal(selection.copyAvailable, false);
  assert.equal(selection.text, "");
});

test("terminal controls keep ownership, Ctrl C, and Enter first and suspend input while selecting", () => {
  assert.deepEqual(terminalControlLabels("controlled", "input").slice(0, 3), ["Release control", "Ctrl C", "Enter"]);
  assert.deepEqual(terminalControlLabels("controlled", "select"), ["Release control", "Done"]);
  assert.deepEqual(terminalControlLabels("controlled", "select", true), ["Release control", "Copy", "Done"]);
  assert.deepEqual(terminalControlLabels("observing", "input"), ["Control"]);
  assert.deepEqual(terminalControlLabels("observing", "input", true), ["Control", "Copy"]);
  assert.deepEqual(terminalControlLabels("controlled_elsewhere", "input"), ["Take over"]);
  assert.deepEqual(terminalControlLabels("controlled_elsewhere", "input", true), ["Take over", "Copy"]);
  assert.equal(terminalControlLabels("controlled", "input", true).includes("Copy"), false);
  assert.deepEqual(terminalControlLabels("retry_exhausted", "input"), []);
  assert.deepEqual(terminalStateActionLabels("retry_exhausted"), ["Try again", "Home"]);
  assert.deepEqual(terminalStateActionLabels("terminal_unavailable"), ["Home"]);
});
