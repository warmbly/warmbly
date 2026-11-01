import assert from "node:assert/strict";
import test from "node:test";
import { summarizeLiveTests } from "./live-tests-summary.mjs";

const events = (...rows) => rows.map((row) => JSON.stringify(row)).join("\n");

test("reports failures, skips and failed packages without counting subtests twice", () => {
  const report = summarizeLiveTests(events(
    { Action: "pass", Package: "app", Test: "TestLivePassed/child" },
    { Action: "pass", Package: "app", Test: "TestLivePassed" },
    { Action: "fail", Package: "repository", Test: "TestLiveFailed/child" },
    { Action: "fail", Package: "repository", Test: "TestLiveFailed" },
    { Action: "fail", Package: "repository" },
    { Action: "skip", Package: "app", Test: "TestLiveSkipped" },
    { Action: "pass", Package: "app", Test: "TestOther" },
  ), "failure");
  assert.equal(report.needsAttention, true);
  assert.match(report.markdown, /\| Passed \| 1 \|/);
  assert.match(report.markdown, /\| Failed \| 1 \|/);
  assert.match(report.markdown, /\| Skipped \| 1 \|/);
  assert.match(report.markdown, /`repository\/TestLiveFailed`/);
  assert.match(report.markdown, /`app\/TestLiveSkipped`/);
  assert.doesNotMatch(report.markdown, /\/child/);
});

test("empty, interrupted and compile-only output cannot look like passing coverage", () => {
  for (const input of ["", "# app\ncompiler error", 'null\n{"Action":"fail","Package":"app"}']) {
    const report = summarizeLiveTests(input, "failure");
    assert.equal(report.needsAttention, true);
    assert.match(report.markdown, /No completed TestLive results/);
  }
  assert.equal(summarizeLiveTests(events({ Action: "pass", Package: "app", Test: "TestLiveOK" }), "cancelled").needsAttention, true);
});

test("successful coverage is clean only when completed tests contain no skips", () => {
  assert.equal(summarizeLiveTests(events({ Action: "pass", Package: "app", Test: "TestLiveOK" }), "success").needsAttention, false);
  assert.equal(summarizeLiveTests(events({ Action: "skip", Package: "app", Test: "TestLiveSkipped" }), "success").needsAttention, true);
});

test("a skipped subtest is actionable even when its parent passes", () => {
  const report = summarizeLiveTests(events(
    { Action: "skip", Package: "app", Test: "TestLiveExample/needs-fixture" },
    { Action: "pass", Package: "app", Test: "TestLiveExample" },
  ), "success");
  assert.equal(report.needsAttention, true);
  assert.match(report.markdown, /\| Passed \| 1 \|/);
  assert.match(report.markdown, /\| Skipped \| 0 \|/);
  assert.match(report.markdown, /`app\/TestLiveExample\/needs-fixture`/);
});
