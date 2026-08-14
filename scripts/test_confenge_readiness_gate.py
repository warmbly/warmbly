#!/usr/bin/env python3
"""Unit tests for confenge_readiness_gate pure logic (no live stack).

Run: python3 scripts/test_confenge_readiness_gate.py
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

# Import under test
sys.path.insert(0, str(Path(__file__).resolve().parent))
import confenge_readiness_gate as gate  # noqa: E402


class TestExitCodes(unittest.TestCase):
    def test_ready_strict_exit_0(self):
        self.assertEqual(
            gate.exit_code_for_verdict("READY_FOR_CONTROLLED_REAL_OUTREACH", report_only=False),
            0,
        )

    def test_not_ready_strict_exit_nonzero(self):
        code = gate.exit_code_for_verdict(
            "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH", report_only=False
        )
        self.assertNotEqual(code, 0)

    def test_report_only_exit_0_even_when_not_ready(self):
        self.assertEqual(
            gate.exit_code_for_verdict(
                "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH", report_only=True
            ),
            0,
        )


class TestAggregateVerdict(unittest.TestCase):
    def test_one_critical_fail_is_not_ready(self):
        gates = {name: gate.GATE_PASS for name in gate.CRITICAL_GATES}
        gates["governor_10h"] = gate.GATE_FAIL
        verdict, blockers = gate.aggregate_verdict(gates)
        self.assertEqual(verdict, "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH")
        self.assertTrue(any("governor_10h=FAIL" in b for b in blockers))
        self.assertNotEqual(
            gate.exit_code_for_verdict(verdict, report_only=False), 0
        )

    def test_one_critical_not_run_is_not_ready(self):
        gates = {name: gate.GATE_PASS for name in gate.CRITICAL_GATES}
        gates["playwright_live"] = gate.GATE_NOT_RUN
        verdict, blockers = gate.aggregate_verdict(gates)
        self.assertEqual(verdict, "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH")
        self.assertTrue(any("playwright_live=NOT_RUN" in b for b in blockers))
        self.assertNotEqual(
            gate.exit_code_for_verdict(verdict, report_only=False), 0
        )

    def test_stale_is_not_ready(self):
        gates = {name: gate.GATE_PASS for name in gate.CRITICAL_GATES}
        gates["mailpit_exact_delivery"] = gate.GATE_STALE
        verdict, blockers = gate.aggregate_verdict(gates)
        self.assertEqual(verdict, "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH")
        self.assertTrue(any("STALE" in b for b in blockers))

    def test_all_pass_is_ready(self):
        gates = {name: gate.GATE_PASS for name in gate.CRITICAL_GATES}
        verdict, blockers = gate.aggregate_verdict(gates)
        self.assertEqual(verdict, "READY_FOR_CONTROLLED_REAL_OUTREACH")
        self.assertEqual(blockers, [])
        self.assertEqual(gate.exit_code_for_verdict(verdict, report_only=False), 0)


class TestNoPriorAcceptedProof(unittest.TestCase):
    def test_go_no_go_never_contains_prior_accepted_proof(self):
        gates = {name: gate.GATE_NOT_RUN for name in gate.CRITICAL_GATES}
        md = gate.build_go_no_go(
            gates,
            {"gate": gate.GATE_FAIL, "reason": "test"},
            False,
            "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH",
            ["playwright_live=NOT_RUN"],
            code_sha="abc123",
        )
        # Forbidden report pattern: table cell PASS justified only by history.
        self.assertNotRegex(md.lower(), r"\|\s*pass\s*\|\s*prior ")
        self.assertNotIn("previously validated", md.lower())
        self.assertNotIn("assumed green", md.lower())
        self.assertIn("NOT_RUN", md)
        self.assertIn("ci_exact_head", md)
        self.assertIn("playwright_live", md)

    def test_source_has_no_inherited_pass_literals(self):
        src = Path(gate.__file__).read_text()
        # Hardcoded external PASS for playwright/governor is forbidden
        self.assertNotIn('"playwright": "PASS"', src)
        self.assertNotIn('"governor": "PASS"', src)
        self.assertNotIn("| PASS | prior accepted proof |", src)
        # exit 0 always on FAIL is forbidden
        self.assertNotIn("# exit 0 always if we wrote honest FAIL", src)


class TestContactHonesty(unittest.TestCase):
    def test_non_example_domain_is_not_enrollable(self):
        c = gate.classify_email_contact(
            {"email": "joao.silva@acme-construtora.com.br"}
        )
        self.assertEqual(c["status"], gate.STATUS_PUBLIC_DISCOVERED)
        self.assertFalse(c["enrollable"])
        self.assertTrue(c["discovered"])

    def test_example_com_is_fixture(self):
        c = gate.classify_email_contact({"email": "lead@example.com"})
        self.assertEqual(c["status"], gate.STATUS_FIXTURE)
        self.assertFalse(c["enrollable"])

    def test_pattern_guess_not_enrollable(self):
        c = gate.classify_email_contact(
            {
                "email": "nome.sobrenome@empresa.com.br",
                "resolution_method": "pattern_guess",
            }
        )
        self.assertEqual(c["status"], gate.STATUS_CANDIDATE_UNVERIFIED)
        self.assertFalse(c["enrollable"])

    def test_verified_is_enrollable(self):
        c = gate.classify_email_contact(
            {
                "email": "contato@empresa.com.br",
                "verification_status": "VERIFIED",
            }
        )
        self.assertTrue(c["enrollable"])
        self.assertEqual(c["status"], gate.STATUS_VERIFIED)

    def test_public_phone_not_whatsapp_eligible(self):
        p = gate.classify_phone_contact(
            {
                "phone": "+5511999999999",
                "verification_status": "OFFICIAL_SOURCE",
                "source": "brasilapi",
            }
        )
        self.assertTrue(p["official_source"])
        self.assertFalse(p["whatsapp_eligible"])

    def test_feed_gate_separates_channel_readiness(self):
        with tempfile.TemporaryDirectory() as td:
            feed = Path(td) / "feed.json"
            feed.write_text(
                json.dumps(
                    {
                        "leads": [
                            {
                                "contacts": [
                                    {"email": "a@real-company.com.br"},
                                    {
                                        "phone": "+5511888777666",
                                        "source": "brasilapi/registry",
                                        "verification_status": "OFFICIAL_SOURCE",
                                    },
                                ]
                            }
                        ]
                    }
                )
            )
            result = gate.contact_gate(feed)
            self.assertEqual(result["gate"], gate.GATE_PASS)
            self.assertTrue(result["contact_discovered"])
            self.assertEqual(result["enrollable_emails"], 0)
            self.assertFalse(result["email_enrollable"])
            self.assertFalse(result["whatsapp_eligible"])


class TestStickyOnlyGates(unittest.TestCase):
    def test_sticky_gates_not_filled_from_evidence_files(self):
        """Re-stamped dnc_sticky.json must not promote Phase M gates when live skipped."""
        contact = {"gate": gate.GATE_PASS, "reason": "test"}
        sticky = {
            k: {"pass": False, "status": gate.GATE_NOT_RUN, "error": "live phase skipped"}
            for k in (
                "no_burst_creates",
                "dnc_sticky",
                "sent_sticky",
                "approval_sticky",
                "replied_sticky",
                "bounced_sticky",
                "process_restart",
            )
        }
        with tempfile.TemporaryDirectory() as td:
            ev = Path(td)
            sha = gate.current_code_sha() or "testsha"
            for name in (
                "dnc_sticky",
                "reimport_sticky",
                "restart_no_burst",
                "reply_cancels_future",
                "playwright_live",
            ):
                payload = {
                    "result": "PASS",
                    "pass": True,
                    "code_sha": sha,
                    "generated_at": "2026-08-08T00:00:00+00:00",
                }
                if name.startswith("dnc") or "sticky" in name or "restart" in name or "reply" in name:
                    payload["source"] = "re-stamped after HEAD tests / live playwright"
                (ev / f"{name}.json").write_text(json.dumps(payload))
            old_ev = gate.EVIDENCE
            try:
                gate.EVIDENCE = ev
                gates = gate.build_critical_gate_map(
                    contact=contact,
                    sticky_assertions=sticky,
                    restart_ok=False,
                    channel_ok=True,
                )
            finally:
                gate.EVIDENCE = old_ev
        for name in (
            "dnc_sticky",
            "reimport_sticky",
            "restart_no_burst",
            "reply_cancels_future",
        ):
            self.assertEqual(
                gates[name],
                gate.GATE_NOT_RUN,
                f"{name} must stay NOT_RUN when live sticky skipped",
            )
        # Non-sticky external can still load PASS from evidence.
        self.assertEqual(gates["playwright_live"], gate.GATE_PASS)


class TestEvidenceStale(unittest.TestCase):
    def test_missing_is_not_run(self):
        info = gate.classify_evidence_file(Path("/nonexistent/evidence.json"))
        self.assertEqual(info["status"], gate.GATE_NOT_RUN)

    def test_wrong_sha_is_stale(self):
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "gov.json"
            p.write_text(
                json.dumps(
                    {
                        "generated_at": "2026-08-07T12:00:00+00:00",
                        "code_sha": "deadbeef" * 5,
                        "result": "PASS",
                    }
                )
            )
            info = gate.classify_evidence_file(p, expected_sha="cafebabe" * 5)
            self.assertEqual(info["status"], gate.GATE_STALE)


class TestPathsPortable(unittest.TestCase):
    def test_no_grok_tmp_defaults_in_source(self):
        src = Path(gate.__file__).read_text()
        self.assertNotIn("/tmp/grok-goal-", src)
        # Absolute machine mounts must not be baked as default FEED/ARTIFACT.
        self.assertNotIn(
            "/tmp/grok-goal-54bfd8993c72",
            src,
        )

    def test_discover_git_root_finds_warmbly(self):
        root = gate.discover_git_root(Path(__file__).resolve().parent)
        self.assertIsNotNone(root)
        self.assertTrue((root / "go.mod").exists())


class TestCLIStrictExit(unittest.TestCase):
    def test_skip_live_strict_exits_nonzero(self):
        """With most gates NOT_RUN, strict mode must not exit 0."""
        script = Path(gate.__file__).resolve()
        env = os.environ.copy()
        env["CONFENGE_GATE_DO_RESTART"] = "0"
        # Isolate evidence so no accidental PASS files
        with tempfile.TemporaryDirectory() as td:
            env["CONFENGE_GATE_EVIDENCE_DIR"] = td
            env["CONFENGE_GATE_ARTIFACT_DIR"] = str(Path(td) / "art")
            proc = subprocess.run(
                [sys.executable, str(script), "--skip-live"],
                capture_output=True,
                text=True,
                env=env,
                timeout=30,
            )
            self.assertNotEqual(
                proc.returncode,
                0,
                f"strict skip-live must be non-zero, stdout={proc.stdout[-500:]}",
            )
            self.assertIn("NOT_READY", proc.stdout)

    def test_report_only_exits_zero(self):
        script = Path(gate.__file__).resolve()
        env = os.environ.copy()
        with tempfile.TemporaryDirectory() as td:
            env["CONFENGE_GATE_EVIDENCE_DIR"] = td
            env["CONFENGE_GATE_ARTIFACT_DIR"] = str(Path(td) / "art")
            proc = subprocess.run(
                [sys.executable, str(script), "--skip-live", "--report-only"],
                capture_output=True,
                text=True,
                env=env,
                timeout=30,
            )
            self.assertEqual(proc.returncode, 0, proc.stderr + proc.stdout)


if __name__ == "__main__":
    unittest.main()
