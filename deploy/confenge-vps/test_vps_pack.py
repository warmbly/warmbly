#!/usr/bin/env python3
"""Structural tests for the CONFENGE VPS deployment pack.

Drives real files under deploy/confenge-vps/ (shipped artifacts), not reimplemented policy.
"""
from __future__ import annotations

import re
import subprocess
import sys
import unittest
from pathlib import Path

PACK = Path(__file__).resolve().parent
ROOT = PACK.parent.parent


class TestConfengeVpsPack(unittest.TestCase):
    def test_required_scripts_exist_and_executable_intent(self) -> None:
        required = [
            "validate.sh",
            "up.sh",
            "down.sh",
            "status.sh",
            "pause.sh",
            "resume.sh",
            "backup.sh",
            "restore.sh",
            "connect-hostinger.sh",
            "prove-hostinger-net.sh",
            "prove-restart.sh",
            "self-smoke.sh",
            "post-smtp-unlock.sh",
            "gen-secrets.sh",
            "install.sh",
            "lib.sh",
            "env.example",
            "docker-compose.override.yml",
        ]
        for name in required:
            path = PACK / name
            self.assertTrue(path.is_file(), f"missing {name}")

    def test_env_example_safety_flags(self) -> None:
        text = (PACK / "env.example").read_text(encoding="utf-8")
        self.assertIn("CONFENGE_GREEN_AUTORUN_ENABLED=false", text)
        self.assertIn("CONFENGE_AUTO_SEND_ENABLED=false", text)
        self.assertIn("CONFENGE_REQUIRE_HUMAN_APPROVAL=true", text)
        self.assertIn("CONFENGE_WHATSAPP_ENABLED=false", text)
        self.assertIn("CONFENGE_RATE_MAX_PER_HOUR=20", text)
        self.assertIn("HOSTINGER_PLAN_CLASS=BUSINESS_EMAIL_STARTER", text)
        self.assertIn("CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT=200", text)
        # Must not raise operational max above 20 in this pack
        for m in re.finditer(r"CONFENGE_RATE_MAX_PER_HOUR=(\d+)", text):
            self.assertLessEqual(int(m.group(1)), 20)

    def test_provider_vs_operational_documented(self) -> None:
        plane = (ROOT / "docs/confenge/vps-execution-plane.md").read_text(encoding="utf-8")
        self.assertIn("provider ceiling ≠ operational target", plane)
        self.assertIn("HOSTINGER_PLAN_CLASS", plane)
        self.assertIn("Business Email Starter", plane)
        self.assertIn("1000", plane)
        self.assertIn("10/h", plane)
        self.assertIn("20/h", plane)
        self.assertNotIn("HOSTINGER_PLAN_CLASS=CPANEL", plane)
        # Must not document cPanel hourly ceiling as this mailbox's plan
        env = (PACK / "env.example").read_text(encoding="utf-8")
        self.assertNotIn("HOSTINGER_PLAN_CLASS=CPANEL", env)
        self.assertIn("BUSINESS_EMAIL_STARTER", env)

    def test_connect_script_uses_read_s_not_argv_password(self) -> None:
        src = (PACK / "connect-hostinger.sh").read_text(encoding="utf-8")
        self.assertIn("read -r -s PASS", src)
        # password must not be passed as curl --data with shell expansion of raw argv pattern
        self.assertNotRegex(src, r"curl.*--password")
        self.assertIn("unset CONFENGE_MAILBOX_PASSWORD", src)
        # JSON body from temp file via --data-binary (never -d "$BODY"; password must not be in argv/ps)
        self.assertIn("--data-binary @", src)
        self.assertNotRegex(src, r'curl[^\n]*-d\s+"\$BODY"')

    def test_no_mta_install(self) -> None:
        for path in PACK.rglob("*"):
            if path.suffix in {".sh", ".yml", ".md", ".example"} and path.is_file():
                text = path.read_text(encoding="utf-8", errors="replace")
                self.assertNotRegex(
                    text,
                    r"apt(-get)?\s+install\s+.*(postfix|exim4|mailcow|mailu)",
                    msg=f"MTA install in {path.name}",
                )

    def test_validate_sh_passes(self) -> None:
        """Run the shipped validate entrypoint (real path)."""
        script = PACK / "validate.sh"
        proc = subprocess.run(
            ["bash", str(script)],
            cwd=str(ROOT),
            capture_output=True,
            text=True,
            timeout=120,
        )
        if proc.returncode != 0:
            self.fail(f"validate.sh exit {proc.returncode}\n{proc.stdout}\n{proc.stderr}")
        self.assertIn("VALIDATE=PASS", proc.stdout)

    def test_docs_inventory_exists(self) -> None:
        inv = ROOT / "docs/confenge/vps-execution-inventory.md"
        self.assertTrue(inv.is_file())
        text = inv.read_text(encoding="utf-8")
        self.assertIn("159.195.18.88", text)
        self.assertIn("warmbly-confenge", text)
        self.assertIn("BUSINESS_EMAIL_STARTER", text)
        self.assertIn("1000", text)
        # network premise recorded (SMTP egress may FAIL on Netcup until unlock)
        self.assertTrue("smtp" in text.lower() and "imap" in text.lower())


if __name__ == "__main__":
    unittest.main()
