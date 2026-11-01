import { appendFileSync, existsSync, readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

export function summarizeLiveTests(input, outcome) {
  const tests = new Map();
  const subtests = new Map();
  const failedPackages = new Set();
  for (const line of input.split("\n")) {
    let event;
    try {
      event = JSON.parse(line);
    } catch {
      continue; // Compiler diagnostics can be plain text alongside go test JSON.
    }
    if (!event || typeof event !== "object") continue;
    if (event.Action === "fail" && typeof event.Package === "string" && !event.Test) {
      failedPackages.add(event.Package);
    }
    if (typeof event.Test !== "string" || !event.Test.startsWith("TestLive")) continue;
    if (!["pass", "fail", "skip"].includes(event.Action)) continue;
    const results = event.Test.includes("/") ? subtests : tests;
    results.set(`${event.Package}/${event.Test}`, event.Action);
  }
  const names = (action) => [...tests].filter(([, result]) => result === action).map(([name]) => name).sort();
  const passed = names("pass");
  const failed = names("fail");
  const skipped = names("skip");
  const skippedSubtests = [...subtests].filter(([, result]) => result === "skip").map(([name]) => name).sort();
  const list = (heading, entries) => [
    `### ${heading}`,
    "",
    ...(entries.length ? entries.slice(0, 200).map((name) => `- \`${name.replaceAll("`", "\\`")}\``) : ["None recorded."]),
    ...(entries.length > 200 ? ["", "Showing the first 200 entries; see the complete output artifact."] : []),
    "",
  ];
  return {
    needsAttention: outcome !== "success" || tests.size === 0 || failed.length > 0 || skipped.length > 0 || skippedSubtests.length > 0 || failedPackages.size > 0,
    markdown: [
      "## PostgreSQL catch-all (advisory)",
      "",
      `Test step outcome: **${outcome || "not started"}**. This job is outside the required CI Status aggregate while #1078 is triaged.`,
      "",
      "| Top-level TestLive results | Count |",
      "| --- | ---: |",
      `| Passed | ${passed.length} |`,
      `| Failed | ${failed.length} |`,
      `| Skipped | ${skipped.length} |`,
      "",
      ...(tests.size === 0 ? ["No completed TestLive results were recorded. Check setup, compilation and timeouts.", ""] : []),
      ...list("Failed packages (including setup or build failures)", [...failedPackages].sort()),
      ...list("Failed tests", failed),
      ...list("Skipped tests and subtests", [...skipped, ...skippedSubtests]),
      "Full JSON events and compiler output are retained in the postgres-live-advisory artifact. Subtests are included there; counts above include each top-level test once.",
      "",
    ].join("\n"),
  };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const file = process.argv[2];
  const report = summarizeLiveTests(file && existsSync(file) ? readFileSync(file, "utf8") : "", process.env.LIVE_TEST_OUTCOME);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, report.markdown);
  else process.stdout.write(report.markdown);
  if (report.needsAttention) console.log("::warning::Advisory PostgreSQL coverage needs triage. See the job summary and complete output artifact.");
}
