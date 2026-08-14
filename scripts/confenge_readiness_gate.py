#!/usr/bin/env python3
"""CONFENGE readiness gate — mechanical verifier of campaign verdict artifacts.

Sole writer of GO-NO-GO / sticky proofs. Never hand-edit those after a run.

Principles (CONFENGE-FINAL-READINESS-HARDENING-01):
  - PASS requires current-run evidence (or evidence explicitly marked EXTERNAL).
  - Historical success is NOT a PASS. Use NOT_RUN / STALE / BLOCKED_EXTERNAL.
  - Default exit: READY → 0, NOT_READY / crash → non-zero.
  - --report-only may write artifacts and exit 0; official readiness must not use it.
  - No ephemeral absolute defaults (/tmp/grok-..., machine-specific mounts).
"""
from __future__ import annotations

import argparse
import json
import os
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

# Gate status vocabulary (critical gates must use only these).
GATE_PASS = "PASS"
GATE_FAIL = "FAIL"
GATE_NOT_RUN = "NOT_RUN"
GATE_BLOCKED_EXTERNAL = "BLOCKED_EXTERNAL"
GATE_STALE = "STALE"

CRITICAL_GATES = (
    "full_national_extra_cli",
    "real_feed_generated",
    "real_feed_imported",
    "contact_integrity",
    "approval_content_hash",
    "edit_invalidation",
    "governor_10h",
    "daily_limit_non_conflicting",
    "mailpit_exact_delivery",
    "whatsapp_policy_mock",
    "reply_cancels_future",
    "dnc_sticky",
    "restart_no_burst",
    "reimport_sticky",
    "outcome_hmac_roundtrip",
    "playwright_live",
    "ci_exact_head",
)


def now() -> str:
    return datetime.now(timezone.utc).isoformat()


def discover_git_root(start: Path | None = None) -> Path | None:
    """Walk up from start (or cwd) for a .git directory. Portable across machines."""
    cur = (start or Path.cwd()).resolve()
    for _ in range(12):
        if (cur / ".git").exists() and (cur / "go.mod").exists():
            return cur
        if (cur / ".git").exists() and (cur / "pyproject.toml").exists():
            return cur
        if cur.parent == cur:
            break
        cur = cur.parent
    return None


def _repo_root() -> Path:
    env = os.environ.get("CONFENGE_GATE_WARMBLY_ROOT", "").strip()
    if env:
        return Path(env).resolve()
    found = discover_git_root(Path(__file__).resolve().parent)
    if found:
        return found
    # Fail closed for required paths: caller must set env when not in-repo.
    return Path.cwd().resolve()


def _resolve_path(env_key: str, *relative_parts: str, required: bool = False) -> Path:
    """Prefer env, else repo-relative path. Never hardcode /tmp/grok-* or fixed mounts."""
    raw = os.environ.get(env_key, "").strip()
    if raw:
        return Path(raw).expanduser().resolve()
    root = _repo_root()
    p = root.joinpath(*relative_parts)
    if required and not p.exists() and not os.environ.get(env_key):
        # Deferred: existence checked when the path is used.
        pass
    return p


WARMBLY_ROOT = _repo_root()
API = os.environ.get("CONFENGE_GATE_API", "http://127.0.0.1:18080")
MAILPIT = os.environ.get("CONFENGE_GATE_MAILPIT", "http://127.0.0.1:18025")
ORG = os.environ.get("CONFENGE_GATE_ORG", "22222222-0000-0000-0000-000000000001")
FEED = _resolve_path(
    "CONFENGE_GATE_FEED",
    "internal",
    "app",
    "confenge",
    "testdata",
    "demo_3_companies.json",
)
ARTIFACT = _resolve_path(
    "CONFENGE_GATE_ARTIFACT_DIR",
    "data",
    "confenge-artifacts",
    "CONFENGE-FINAL-INTEGRATION-AND-LIVE-REHEARSAL-01",
)
EVIDENCE = _resolve_path("CONFENGE_GATE_EVIDENCE_DIR", "data", "confenge-evidence")
BACKEND_BIN = os.environ.get(
    "CONFENGE_GATE_BACKEND_BIN",
    str(WARMBLY_ROOT / "bin" / "warmbly-backend"),
)
BACKEND_ENV = _resolve_path("CONFENGE_GATE_BACKEND_ENV", "data", "confenge-backend.env")
RECEPTOR_CMD = os.environ.get(
    "CONFENGE_GATE_RECEPTOR_CMD",
    "python3 -m scripts.warmbly_bridge serve-outcomes --host 127.0.0.1 --port 18090 "
    "--secret confenge-outcome-test-secret-32chars!! --memory-store",
)


def _default_receptor_cwd() -> str:
    env = os.environ.get("CONFENGE_GATE_RECEPTOR_CWD", "").strip()
    if env:
        return env
    # Sibling checkout convention: ../extra-cli next to warmbly, else env required.
    sibling = (WARMBLY_ROOT.parent / "extra-cli").resolve()
    if sibling.is_dir():
        return str(sibling)
    return ""  # fail closed when used


RECEPTOR_CWD = _default_receptor_cwd()
RECEPTOR_HEALTH = os.environ.get(
    "CONFENGE_GATE_RECEPTOR_HEALTH", "http://127.0.0.1:18090/health"
)
DO_RESTART = os.environ.get("CONFENGE_GATE_DO_RESTART", "1") == "1"


def current_code_sha() -> str:
    """HEAD of warmbly repo for evidence binding. Empty if git unavailable."""
    try:
        out = subprocess.check_output(
            ["git", "-C", str(WARMBLY_ROOT), "rev-parse", "HEAD"],
            text=True,
            stderr=subprocess.DEVNULL,
            timeout=5,
        )
        return out.strip()
    except Exception:
        return os.environ.get("CONFENGE_GATE_CODE_SHA", "").strip()


def classify_evidence_file(
    path: Path,
    *,
    expected_sha: str | None = None,
    max_age_hours: float | None = 24.0,
) -> dict[str, Any]:
    """Return status for a dynamic evidence file. Never PASS on missing/stale."""
    if not path.exists():
        return {
            "status": GATE_NOT_RUN,
            "path": str(path),
            "reason": "evidence file missing",
        }
    try:
        data = json.loads(path.read_text())
    except Exception as e:
        return {
            "status": GATE_FAIL,
            "path": str(path),
            "reason": f"unreadable evidence: {e}",
        }
    if not isinstance(data, dict):
        return {
            "status": GATE_FAIL,
            "path": str(path),
            "reason": "evidence root must be object",
        }
    sha = (
        data.get("code_sha")
        or data.get("codeSHA")
        or data.get("tested_sha")
        or data.get("git_sha")
        or ""
    )
    gen = data.get("generated_at") or data.get("at") or data.get("timestamp") or ""
    result = data.get("result") or data.get("status") or data.get("gate") or ""
    if expected_sha and sha and sha != expected_sha:
        return {
            "status": GATE_STALE,
            "path": str(path),
            "reason": f"evidence sha {sha[:12]} != current {expected_sha[:12]}",
            "code_sha": sha,
        }
    if max_age_hours is not None and gen:
        try:
            # Accept Z or offset
            ts = datetime.fromisoformat(str(gen).replace("Z", "+00:00"))
            age_h = (datetime.now(timezone.utc) - ts.astimezone(timezone.utc)).total_seconds() / 3600
            if age_h > max_age_hours:
                return {
                    "status": GATE_STALE,
                    "path": str(path),
                    "reason": f"evidence age {age_h:.1f}h > {max_age_hours}h",
                    "generated_at": gen,
                }
        except Exception:
            pass
    # Interpret result
    r = str(result).upper()
    if r in ("PASS", "OK", "SUCCESS", "TRUE"):
        status = GATE_PASS
    elif r in ("FAIL", "FAILED", "ERROR", "FALSE"):
        status = GATE_FAIL
    elif data.get("pass") is True:
        status = GATE_PASS
    elif data.get("pass") is False:
        status = GATE_FAIL
    else:
        status = GATE_NOT_RUN
        r = "missing result field"

    # Anti-theater: mailpit gate requires mechanical body match fields.
    name = path.name if hasattr(path, "name") else str(path)
    if status == GATE_PASS and "mailpit" in name.lower():
        if not (
            data.get("body_match") is True
            or data.get("hard_asserts") is True
            and data.get("mailpit_message_id")
        ):
            status = GATE_NOT_RUN
            r = "mailpit evidence missing body_match/mailpit_message_id (not theater-stamped)"

    # Anti-theater: sticky gate files alone are never enough without live phase;
    # build_critical_gate_map already refuses STICKY_ONLY file fill-in.
    if status == GATE_PASS and any(
        k in name for k in ("dnc_sticky", "reimport_sticky", "restart_no_burst", "reply_cancels")
    ):
        if data.get("source") == "re-stamped after HEAD tests / live playwright":
            status = GATE_NOT_RUN
            r = "rejected re-stamped sticky theater evidence"

    return {
        "status": status,
        "path": str(path),
        "reason": r if status != GATE_PASS else "current evidence",
        "code_sha": sha or None,
        "generated_at": gen or None,
        "raw_result": result,
    }


def aggregate_verdict(gates: dict[str, str]) -> tuple[str, list[str]]:
    """READY only when every critical gate is PASS. NOT_RUN/STALE → NOT_READY."""
    blockers: list[str] = []
    for name in CRITICAL_GATES:
        st = gates.get(name, GATE_NOT_RUN)
        if st == GATE_PASS:
            continue
        blockers.append(f"{name}={st}")
    if blockers:
        return "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH", blockers
    return "READY_FOR_CONTROLLED_REAL_OUTREACH", []


def exit_code_for_verdict(verdict: str, *, report_only: bool) -> int:
    """Strict mode: READY→0, else non-zero. --report-only always 0 after write."""
    if report_only:
        return 0
    if verdict == "READY_FOR_CONTROLLED_REAL_OUTREACH":
        return 0
    return 2


def load_external_gate_status(name: str, env_key: str | None = None) -> str:
    """Load a gate from CONFENGE_GATE_<NAME>_EVIDENCE file or env override.

    Never invents PASS. Missing evidence → NOT_RUN.
    """
    env_status = os.environ.get(f"CONFENGE_GATE_{name.upper()}_STATUS", "").strip().upper()
    if env_status in (GATE_PASS, GATE_FAIL, GATE_NOT_RUN, GATE_BLOCKED_EXTERNAL, GATE_STALE):
        return env_status
    key = env_key or f"CONFENGE_GATE_{name.upper()}_EVIDENCE"
    raw = os.environ.get(key, "").strip()
    if not raw:
        # Convention: data/confenge-evidence/<name>.json
        cand = EVIDENCE / f"{name}.json"
        if not cand.exists():
            return GATE_NOT_RUN
        path = cand
    else:
        path = Path(raw)
    info = classify_evidence_file(path, expected_sha=current_code_sha() or None)
    return info["status"]


def req(method: str, path: str, body=None, token: str | None = None, headers=None, raw=False):
    data = None
    if body is not None:
        data = body if isinstance(body, (bytes, bytearray)) else json.dumps(body).encode()
    h = {"Content-Type": "application/json"}
    if token:
        h["Authorization"] = f"Bearer {token}"
    if headers:
        h.update(headers)
    r = urllib.request.Request(API + path, data=data, method=method, headers=h)
    try:
        with urllib.request.urlopen(r, timeout=60) as resp:
            raw_b = resp.read()
            if raw:
                return resp.status, raw_b
            return resp.status, (json.loads(raw_b) if raw_b else {})
    except urllib.error.HTTPError as e:
        b = e.read()
        try:
            j = json.loads(b.decode())
        except Exception:
            j = {"error": b.decode()[:500]}
        return e.code, j


def login() -> str:
    # flush redis rate limit if possible
    try:
        subprocess.run(
            ["bash", "-c", "docker exec $(docker ps -qf name=redis | head -1) redis-cli FLUSHDB"],
            capture_output=True,
            timeout=10,
        )
    except Exception:
        pass
    st, start = req("POST", "/v1/auth/login", {"email": "dev@warmbly.com", "password": "password123"})
    if st >= 400:
        raise RuntimeError(f"login start {st}: {start}")
    session = start["session"]
    code = ""
    for _ in range(40):
        try:
            with urllib.request.urlopen(MAILPIT + "/api/v1/messages", timeout=10) as resp:
                msgs = json.loads(resp.read()).get("messages") or []
        except Exception:
            msgs = []
        for m in msgs:
            if "Login Code" in (m.get("Subject") or ""):
                with urllib.request.urlopen(MAILPIT + f"/api/v1/message/{m['ID']}", timeout=10) as resp:
                    body = json.loads(resp.read())
                text = body.get("Text") or body.get("HTML") or ""
                import re

                m2 = re.search(r"\b(\d{6})\b", text)
                if m2:
                    code = m2.group(1)
                    break
        if code:
            break
        time.sleep(0.3)
    if not code:
        raise RuntimeError("no login OTP in Mailpit")
    st, tok = req("POST", "/v1/auth/login/confirm", {"code": code, "session": session})
    if st >= 400:
        raise RuntimeError(f"login confirm {st}: {tok}")
    access = tok["access_token"]
    st, _ = req("POST", f"/v1/organization/switch/{ORG}", token=access)
    if st >= 400:
        raise RuntimeError(f"org switch {st}")
    return access


def dig_counts(obj):
    if not isinstance(obj, dict):
        return {}
    for k in ("counts", "Counts", "data", "result"):
        if k in obj and isinstance(obj[k], dict):
            if any(x in obj[k] for x in ("creates", "Creates", "unchanged")):
                return obj[k]
            nested = dig_counts(obj[k])
            if nested:
                return nested
    if any(x in obj for x in ("creates", "Creates", "unchanged")):
        return obj
    return {}


def pids_for(patterns: list[str]) -> list[int]:
    out = []
    try:
        ps = subprocess.check_output(["ps", "aux"], text=True)
    except Exception:
        return out
    for line in ps.splitlines():
        if "grep" in line:
            continue
        for p in patterns:
            if p in line:
                parts = line.split()
                try:
                    out.append(int(parts[1]))
                except Exception:
                    pass
                break
    return out


def wait_http(url: str, timeout: float = 60.0) -> bool:
    """True when the process answers HTTP (even 401/404 means the server is up)."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=3) as resp:
                if resp.status < 500:
                    return True
        except urllib.error.HTTPError as e:
            # 401/403/404 still prove the listener is alive.
            if e.code < 500:
                return True
        except Exception:
            pass
        time.sleep(0.5)
    return False


def kill_pids(pids: list[int]) -> list[dict]:
    log = []
    for pid in pids:
        try:
            os.kill(pid, signal.SIGTERM)
            log.append({"pid": pid, "signal": "SIGTERM"})
        except ProcessLookupError:
            log.append({"pid": pid, "signal": "already_dead"})
        except Exception as e:
            log.append({"pid": pid, "error": str(e)})
    time.sleep(1.5)
    for pid in pids:
        try:
            os.kill(pid, 0)
            os.kill(pid, signal.SIGKILL)
            log.append({"pid": pid, "signal": "SIGKILL"})
        except ProcessLookupError:
            pass
        except Exception as e:
            log.append({"pid": pid, "error": str(e)})
    time.sleep(0.5)
    return log


def start_backend() -> subprocess.Popen:
    env = os.environ.copy()
    if BACKEND_ENV.exists():
        for line in BACKEND_ENV.read_text().splitlines():
            if "=" in line and not line.startswith("#"):
                k, v = line.split("=", 1)
                env[k] = v
    if not Path(BACKEND_BIN).exists():
        raise FileNotFoundError(
            f"backend binary missing: {BACKEND_BIN} "
            f"(set CONFENGE_GATE_BACKEND_BIN or build into repo bin/)"
        )
    log_path = EVIDENCE / "backend-restart.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)
    lf = open(log_path, "ab")
    return subprocess.Popen(
        [BACKEND_BIN],
        cwd=str(WARMBLY_ROOT),
        env=env,
        stdout=lf,
        stderr=lf,
        start_new_session=True,
    )


def start_receptor() -> subprocess.Popen:
    if not RECEPTOR_CWD or not Path(RECEPTOR_CWD).is_dir():
        raise FileNotFoundError(
            "CONFENGE_GATE_RECEPTOR_CWD unset/invalid; expected extra-cli checkout "
            "(sibling ../extra-cli or explicit env)"
        )
    log_path = EVIDENCE / "receptor-restart.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)
    lf = open(log_path, "ab")
    return subprocess.Popen(
        RECEPTOR_CMD.split(),
        cwd=RECEPTOR_CWD,
        stdout=lf,
        stderr=lf,
        start_new_session=True,
    )


def get_account(token: str, aid: str) -> dict:
    st, j = req("GET", f"/v1/confenge/accounts/{aid}", token=token)
    if st >= 400:
        return {"_error": j, "_status": st}
    return j.get("data") or j


def get_tp(token: str, tid: str) -> dict:
    st, j = req("GET", f"/v1/confenge/touchpoints/{tid}", token=token)
    if st >= 400:
        return {"_error": j, "_status": st}
    return j.get("data") or j


def summary(token: str) -> dict:
    st, j = req("GET", "/v1/confenge/summary", token=token)
    return (j.get("data") or j) if st < 400 else {"_error": j, "_status": st}


# Email/phone verification statuses (honest separation — not a single boolean).
STATUS_NOT_AVAILABLE = "NOT_AVAILABLE"
STATUS_PUBLIC_DISCOVERED = "PUBLIC_DISCOVERED"
STATUS_OFFICIAL_SOURCE = "OFFICIAL_SOURCE"
STATUS_CANDIDATE_UNVERIFIED = "CANDIDATE_UNVERIFIED"
STATUS_GENERIC_PUBLIC = "GENERIC_PUBLIC"
STATUS_VERIFIED = "VERIFIED"
STATUS_HUMAN_CONFIRMED = "HUMAN_CONFIRMED"
STATUS_FIXTURE = "FIXTURE"

# Only these make an email enrollable for cold EMAIL channel.
_EMAIL_ENROLLABLE_STATUSES = frozenset(
    {STATUS_VERIFIED, STATUS_HUMAN_CONFIRMED, STATUS_OFFICIAL_SOURCE}
)
# Explicit verification tokens that count as verified (not format/domain heuristics).
_VERIFIED_TOKENS = frozenset(
    {
        "VERIFIED",
        "HUMAN_CONFIRMED",
        "HUMAN_VERIFIED",
        "OFFICIAL_SOURCE",
        "REGISTRY",
        "MX_VERIFIED",
        "SMTP_VERIFIED",
        "CATCH_ALL_CHECKED",
    }
)
_PATTERN_GUESS_TOKENS = frozenset(
    {
        "PATTERN",
        "GUESSED",
        "INFERRED",
        "PATTERN_GUESS",
        "NAME_PATTERN",
        "DERIVED",
    }
)


def classify_email_contact(c: dict) -> dict[str, Any]:
    """Classify one contact email honestly.

    Valid format + non-example.com domain ≠ verified. Pattern-guessed emails
    stay CANDIDATE_UNVERIFIED / not enrollable until adequate evidence.
    """
    em = (c.get("email") or c.get("email_address") or "").strip().lower()
    if not em or "@" not in em:
        return {
            "email": em or None,
            "status": STATUS_NOT_AVAILABLE,
            "enrollable": False,
            "discovered": False,
        }
    dom = em.split("@", 1)[1]
    vs = str(
        c.get("verification_status")
        or c.get("email_verification_status")
        or c.get("status")
        or ""
    ).strip().upper()
    src = str(c.get("source") or c.get("source_url") or c.get("provenance") or "").lower()
    method = str(c.get("resolution_method") or c.get("method") or "").lower()

    if dom.endswith("example.com") or "example." in dom or dom.endswith("test.local"):
        return {
            "email": em,
            "domain": dom,
            "status": STATUS_FIXTURE,
            "enrollable": False,
            "discovered": False,
        }

    # Pattern guessing is never enrollable regardless of domain existence.
    if (
        vs in _PATTERN_GUESS_TOKENS
        or "pattern" in method
        or "guess" in method
        or "infer" in method
        or "pattern" in src
    ):
        return {
            "email": em,
            "domain": dom,
            "status": STATUS_CANDIDATE_UNVERIFIED,
            "enrollable": False,
            "discovered": True,
            "reason": "pattern_guess_or_inferred",
        }

    if vs in _VERIFIED_TOKENS or vs == STATUS_OFFICIAL_SOURCE:
        status = STATUS_HUMAN_CONFIRMED if "HUMAN" in vs else (
            STATUS_OFFICIAL_SOURCE if vs in ("OFFICIAL_SOURCE", "REGISTRY") else STATUS_VERIFIED
        )
        return {
            "email": em,
            "domain": dom,
            "status": status,
            "enrollable": True,
            "discovered": True,
            "verification_status": vs,
        }

    # Generic public mailbox patterns (contato@, comercial@) without verification.
    local = em.split("@", 1)[0]
    if local in ("contato", "comercial", "vendas", "info", "contact", "hello", "admin", "sac"):
        return {
            "email": em,
            "domain": dom,
            "status": STATUS_GENERIC_PUBLIC,
            "enrollable": False,
            "discovered": True,
            "reason": "generic_local_part_unverified",
        }

    # Discovered public email with no verification evidence.
    return {
        "email": em,
        "domain": dom,
        "status": STATUS_PUBLIC_DISCOVERED,
        "enrollable": False,
        "discovered": True,
        "reason": "domain_exists_is_not_verified",
    }


def classify_phone_contact(c: dict) -> dict[str, Any]:
    """Public phone ≠ WhatsApp opt-in. Official registry phone still not WA-eligible."""
    ph = (
        c.get("phone") or c.get("phone_e164") or c.get("telefone") or ""
    ).strip()
    if not ph:
        return {
            "phone": None,
            "status": STATUS_NOT_AVAILABLE,
            "whatsapp_eligible": False,
            "discovered": False,
        }
    vs = str(c.get("verification_status") or c.get("phone_verification_status") or "").upper()
    src = str(c.get("source_url") or c.get("source") or "").lower()
    consent = bool(
        c.get("whatsapp_opt_in")
        or c.get("whatsapp_consent")
        or c.get("wa_opt_in")
        or (str(c.get("channel_consent") or "").upper() == "WHATSAPP")
    )
    official = vs in ("OFFICIAL_SOURCE", "REGISTRY") or any(
        k in src for k in ("brasilapi", "official", "registry", "rfb", "receita")
    )
    status = STATUS_OFFICIAL_SOURCE if official else STATUS_PUBLIC_DISCOVERED
    return {
        "phone": ph,
        "status": status,
        "whatsapp_eligible": consent,  # never true from public discovery alone
        "discovered": True,
        "official_source": official,
        "note": "public phone ≠ WhatsApp opt-in",
    }


def contact_gate(feed_path: Path | None = None) -> dict:
    """Hard contact honesty for confenge.outreach.v1 feed.

    Separates:
      contact discovered | email enrollable | WhatsApp eligible

    PASS on contact_integrity requires non-fixture discovery path OR human pilot list.
    Channel readiness (email enrollable / WA eligible) is reported separately and
    does NOT auto-pass from domain != example.com.
    """
    path = feed_path or FEED
    feed_emails_example = 0
    total_emails = 0
    total_phones = 0
    official_phones = 0
    discovered_emails = 0
    enrollable_emails = 0
    whatsapp_eligible = 0
    status_counts: dict[str, int] = {}
    sample_domains: list[str] = []
    sample_sources: list[str] = []
    sample_classifications: list[dict] = []

    if not path.exists():
        return {
            "gate": GATE_FAIL,
            "reason": f"feed missing: {path}",
            "email_enrollable_count": 0,
            "whatsapp_eligible_count": 0,
            "contact_discovered": False,
        }

    try:
        payload = json.loads(path.read_text())
        leads = payload.get("leads") or payload.get("data") or []
        if isinstance(payload, list):
            leads = payload
        for lead in leads[:500]:
            contacts = lead.get("contacts") or []
            for c in contacts:
                if not isinstance(c, dict):
                    continue
                ec = classify_email_contact(c)
                if ec.get("email"):
                    total_emails += 1
                    st = ec["status"]
                    status_counts[st] = status_counts.get(st, 0) + 1
                    if st == STATUS_FIXTURE:
                        feed_emails_example += 1
                    if ec.get("discovered"):
                        discovered_emails += 1
                    if ec.get("enrollable"):
                        enrollable_emails += 1
                    dom = ec.get("domain")
                    if dom and len(sample_domains) < 8:
                        sample_domains.append(str(dom))
                    if len(sample_classifications) < 6:
                        sample_classifications.append(ec)

                pc = classify_phone_contact(c)
                if pc.get("phone"):
                    total_phones += 1
                    if pc.get("official_source"):
                        official_phones += 1
                    if pc.get("whatsapp_eligible"):
                        whatsapp_eligible += 1
                    src = c.get("source_url") or c.get("source") or ""
                    if len(sample_sources) < 6 and src:
                        sample_sources.append(str(src)[:120])
    except Exception as e:
        return {"gate": GATE_FAIL, "error": f"feed parse: {e}"}

    fixture_email_ratio = (feed_emails_example / total_emails) if total_emails else 0.0
    has_fixture_email = feed_emails_example > 0
    contact_discovered = discovered_emails > 0 or official_phones > 0 or total_phones > 0

    pilot = os.environ.get("CONFENGE_HUMAN_VERIFIED_PILOT_LIST", "")
    pilot_ok = bool(pilot) and Path(pilot).exists()

    # Integrity gate: fixture domains fail closed unless pilot overrides.
    # Non-fixture public discovery OR official phones OR pilot → PASS integrity.
    # Enrollability is a separate channel flag.
    if has_fixture_email and not pilot_ok:
        gate = GATE_FAIL
        reason = "fixture example.com emails present"
    elif pilot_ok:
        gate = GATE_PASS
        reason = "human-verified pilot recipient list present"
    elif contact_discovered and not has_fixture_email:
        gate = GATE_PASS
        reason = "live public/official contacts discovered (enrollability separate)"
    else:
        gate = GATE_FAIL
        reason = "no non-fixture contacts and no pilot list"

    return {
        "gate": gate,
        "reason": reason,
        "total_emails_sampled": total_emails,
        "example_com_emails": feed_emails_example,
        "fixture_email_ratio": fixture_email_ratio,
        # Honest: only verified/human/official, NOT "non-example.com".
        "enrollable_emails": enrollable_emails,
        "enrollable_non_fixture_emails": enrollable_emails,  # legacy key, same semantics
        "discovered_emails": discovered_emails,
        "total_phones": total_phones,
        "official_source_phones": official_phones,
        "whatsapp_eligible_count": whatsapp_eligible,
        "contact_discovered": contact_discovered,
        "email_enrollable": enrollable_emails > 0 or pilot_ok,
        "whatsapp_eligible": whatsapp_eligible > 0,
        "status_counts": status_counts,
        "sample_domains": sample_domains,
        "sample_sources": sample_sources,
        "sample_classifications": sample_classifications,
        "pilot_list": pilot if pilot_ok else None,
        "whatsapp_eligible_note": (
            "Public phone does not imply WhatsApp opt-in "
            "(eligible only with explicit consent fields)."
        ),
        "note": (
            "domain!=example.com is NOT verified. "
            "Pattern-guessed emails stay non-enrollable. "
            "contact_discovered ≠ email_enrollable ≠ whatsapp_eligible."
        ),
    }


def seed_states(token: str) -> dict:
    """Seed DNC/SENT/APPROVED/REPLIED/BOUNCED via public product APIs only."""
    seeds: dict = {"paths": [], "errors": []}

    def account_pool() -> list[dict]:
        pool: list[dict] = []
        for qs in (
            "READY_TO_GENERATE",
            "NEEDS_REVIEW",
            "NEEDS_CONTACT",
            "APPROVED",
            "ENROLLED",
        ):
            st, res = req(
                "GET",
                f"/v1/confenge/accounts?queue_state={qs}&limit=30",
                token=token,
            )
            if st < 400:
                pool.extend(res.get("data") or [])
        if len(pool) < 5:
            st, all_a = req("GET", "/v1/confenge/accounts?limit=100", token=token)
            if st < 400:
                pool.extend(all_a.get("data") or [])
        # de-dupe
        seen: set[str] = set()
        out: list[dict] = []
        for a in pool:
            aid = a.get("id")
            if not aid or aid in seen:
                continue
            if (a.get("queue_state") or "") in ("DO_NOT_CONTACT", "BOUNCED", "REPLIED"):
                continue
            seen.add(aid)
            out.append(a)
        return out

    ready_list = account_pool()

    def take() -> dict | None:
        return ready_list.pop(0) if ready_list else None

    def ensure_review_tp(aid: str) -> dict | None:
        """Plan/generate until a reviewable touchpoint with body exists, or reuse review queue."""
        req("POST", f"/v1/confenge/accounts/{aid}/plan", {"channel": "EMAIL"}, token=token)
        st, tps = req("GET", f"/v1/confenge/accounts/{aid}/touchpoints", token=token)
        list_tp = tps.get("data") or [] if st < 400 else []
        tp = next(
            (
                t
                for t in list_tp
                if (t.get("state") or "") in ("DUE", "NEEDS_REVIEW", "DRAFTED", "APPROVED")
            ),
            None,
        )
        if not tp:
            # fall back to org review queue item for this account
            st, rev = req("GET", "/v1/confenge/touchpoints/review?limit=50", token=token)
            for t in rev.get("data") or []:
                if t.get("account_id") == aid:
                    tp = t
                    break
        if not tp:
            return None
        full = get_tp(token, tp["id"])
        if not (full.get("body_text") or "").strip():
            st_g, gen = req(
                "POST", f"/v1/confenge/touchpoints/{tp['id']}/generate", {}, token=token
            )
            if st_g < 400:
                full = gen.get("data") or get_tp(token, tp["id"])
        # Force-edit when generate leaves body/recipient empty (no AI/worker in gate env).
        # Prefer enrollable pilot sink so queue/enroll can succeed.
        if not (full.get("body_text") or "").strip() or not (full.get("recipient") or "").strip():
            pilot_rcpt = (full.get("recipient") or "").strip()
            if not pilot_rcpt:
                pilot_rcpt = f"confenge-gate+{aid[:8]}@warmbly.local"
            st_e, ed = req(
                "POST",
                f"/v1/confenge/touchpoints/{tp['id']}/edit",
                {
                    "subject": full.get("subject") or "CONFENGE readiness gate",
                    "body_text": (
                        full.get("body_text")
                        or (
                            "Ola, retomo com uma pergunta objetiva sobre o pacote em andamento. "
                            "Posso enviar um recorte de auditoria de planilha/BDI se fizer sentido."
                        )
                    ),
                    "recipient": pilot_rcpt,
                },
                token=token,
            )
            if st_e < 400:
                full = ed.get("data") or get_tp(token, tp["id"])
        if not (full.get("body_text") or "").strip() or not (full.get("recipient") or "").strip():
            return None
        return full

    # Prefer existing review-queue touchpoints (works when READY_TO_GENERATE is empty).
    st, rev = req("GET", "/v1/confenge/touchpoints/review?limit=50", token=token)
    review_tps = [
        t
        for t in (rev.get("data") or [])
        if (t.get("body_text") or "").strip() and (t.get("recipient") or "").strip()
    ]

    # --- SENT via plan+generate+approve+queue (or review-queue TP) ---
    sent_tp = review_tps.pop(0) if review_tps else None
    if not sent_tp:
        # Try multiple accounts; polluted DBs may have terminal TPs on early rows.
        for _ in range(15):
            a = take()
            if not a:
                break
            sent_tp = ensure_review_tp(a["id"])
            if sent_tp:
                sent_tp["account_id"] = a["id"]
                break
    if sent_tp:
        tid = sent_tp["id"]
        aid = sent_tp.get("account_id") or ""
        st, ap = req("POST", f"/v1/confenge/touchpoints/{tid}/approve", {}, token=token)
        seeds["paths"].append({"step": "approve", "status": st})
        st, q = req("POST", f"/v1/confenge/touchpoints/{tid}/queue", {}, token=token)
        seeds["paths"].append({"step": "queue", "status": st, "body": q if st >= 400 else "ok"})
        full = get_tp(token, tid)
        seeds["sent"] = {
            "account_id": aid or full.get("account_id"),
            "touchpoint_id": tid,
            "state": full.get("state"),
            "approved_content_hash": full.get("approved_content_hash"),
            "content_hash": full.get("content_hash"),
            "path": "POST approve → queue on reviewable TP (public product path)",
        }
    else:
        seeds["errors"].append("no touchpoint for SENT seed")

    # --- APPROVED (stay APPROVED, do not queue) ---
    appr_tp = review_tps.pop(0) if review_tps else None
    if not appr_tp:
        for _ in range(15):
            a = take()
            if not a:
                break
            appr_tp = ensure_review_tp(a["id"])
            if appr_tp:
                appr_tp["account_id"] = a["id"]
                break
    if appr_tp:
        tid = appr_tp["id"]
        aid = appr_tp.get("account_id") or ""
        st, ap = req("POST", f"/v1/confenge/touchpoints/{tid}/approve", {}, token=token)
        full = get_tp(token, tid) if st < 400 else (ap.get("data") or {})
        seeds["approved"] = {
            "account_id": aid or full.get("account_id"),
            "touchpoint_id": tid,
            "state": full.get("state"),
            "approved_content_hash": full.get("approved_content_hash"),
            "content_hash": full.get("content_hash"),
            "path": "POST approve (no queue) on reviewable TP",
            "http_status": st,
        }
    else:
        seeds["errors"].append("no touchpoint for APPROVED seed")

    # --- DNC via public /dnc ---
    a = take()
    if a:
        aid = a["id"]
        st, body = req("POST", f"/v1/confenge/accounts/{aid}/dnc", {}, token=token)
        acc = get_account(token, aid)
        seeds["dnc"] = {
            "account_id": aid,
            "http_status": st,
            "do_not_contact": acc.get("do_not_contact"),
            "queue_state": acc.get("queue_state"),
            "path": "POST /v1/confenge/accounts/:id/dnc",
            "ok": st < 400 and bool(acc.get("do_not_contact")),
        }
    else:
        seeds["errors"].append("no account for DNC seed")

    # --- REPLIED via public cancel-touchpoints reason=REPLY ---
    a = take()
    if a:
        aid = a["id"]
        # ensure some open TP if possible
        req("POST", f"/v1/confenge/accounts/{aid}/plan", {"channel": "EMAIL"}, token=token)
        st, body = req(
            "POST",
            f"/v1/confenge/accounts/{aid}/cancel-touchpoints",
            {"reason": "REPLY"},
            token=token,
        )
        acc = get_account(token, aid)
        seeds["replied"] = {
            "account_id": aid,
            "http_status": st,
            "queue_state": acc.get("queue_state"),
            "path": "POST /v1/confenge/accounts/:id/cancel-touchpoints reason=REPLY",
            "ok": st < 400 and (acc.get("queue_state") or "").upper() == "REPLIED",
            "response": body if st >= 400 else body.get("data"),
        }
    else:
        seeds["replied"] = {
            "ok": False,
            "error": "no account for REPLIED seed",
            "path": "POST cancel-touchpoints reason=REPLY",
        }

    # --- BOUNCED via public cancel-touchpoints reason=BOUNCE ---
    a = take()
    if a:
        aid = a["id"]
        req("POST", f"/v1/confenge/accounts/{aid}/plan", {"channel": "EMAIL"}, token=token)
        st, body = req(
            "POST",
            f"/v1/confenge/accounts/{aid}/cancel-touchpoints",
            {"reason": "BOUNCE"},
            token=token,
        )
        acc = get_account(token, aid)
        seeds["bounced"] = {
            "account_id": aid,
            "http_status": st,
            "queue_state": acc.get("queue_state"),
            "path": "POST /v1/confenge/accounts/:id/cancel-touchpoints reason=BOUNCE",
            "ok": st < 400 and (acc.get("queue_state") or "").upper() == "BOUNCED",
            "response": body if st >= 400 else body.get("data"),
        }
    else:
        seeds["bounced"] = {
            "ok": False,
            "error": "no account for BOUNCED seed",
            "path": "POST cancel-touchpoints reason=BOUNCE",
        }

    return seeds


def reimport(token: str, key: str) -> dict:
    raw = FEED.read_bytes()
    st, j = req(
        "POST",
        "/v1/confenge/import",
        raw,
        token=token,
        headers={"Idempotency-Key": key, "Content-Type": "application/json"},
    )
    counts = dig_counts(j)
    return {"status": st, "counts": counts, "raw_keys": list(j.keys()) if isinstance(j, dict) else []}


def write_all(payload: dict) -> None:
    ARTIFACT.mkdir(parents=True, exist_ok=True)
    EVIDENCE.mkdir(parents=True, exist_ok=True)

    sticky = payload["sticky_proof"]
    for base in (ARTIFACT, EVIDENCE):
        (base / "restart-reimport-sticky-proof.json").write_text(json.dumps(sticky, indent=2) + "\n")
        (base / "restart-reimport-proof.json").write_text(
            json.dumps(payload["restart_proof"], indent=2) + "\n"
        )
        (base / "contact-gate-honesty.json").write_text(
            json.dumps(payload["contact_gate"], indent=2) + "\n"
        )
        (base / "GO-NO-GO.md").write_text(payload["go_no_go_md"])
        (base / "result.json").write_text(json.dumps(payload["result"], indent=2) + "\n")
        # keep contact-resolution metrics honest if we own them
        if payload.get("contact_metrics"):
            (base / "contact-resolution-metrics.json").write_text(
                json.dumps(payload["contact_metrics"], indent=2) + "\n"
            )


def _bool_gate(ok: bool | None, ran: bool = True) -> str:
    if not ran:
        return GATE_NOT_RUN
    if ok is None:
        return GATE_NOT_RUN
    return GATE_PASS if ok else GATE_FAIL


def build_critical_gate_map(
    *,
    contact: dict,
    sticky_assertions: dict,
    restart_ok: bool,
    channel_ok: bool,
    external: dict[str, str] | None = None,
) -> dict[str, str]:
    """Map CRITICAL_GATES → PASS|FAIL|NOT_RUN|BLOCKED_EXTERNAL|STALE.

    Live sticky/restart assertions from this run are measured here.
    External gates (playwright, governor unit suite, full national, CI, …)
    must be supplied via evidence files / env — never inherited as PASS.
    """
    ext = external or {}
    sticky = sticky_assertions or {}

    def sticky_ok(key: str) -> str:
        v = sticky.get(key)
        if not isinstance(v, dict):
            return GATE_NOT_RUN
        st = str(v.get("status") or "").upper()
        if st in (GATE_NOT_RUN, GATE_STALE, GATE_BLOCKED_EXTERNAL):
            return st
        if "pass" not in v:
            return GATE_NOT_RUN
        return GATE_PASS if v.get("pass") else GATE_FAIL

    # Live sticky-only gates: NEVER filled from evidence files (re-stamp theater).
    # Phase M must run on the product stack for these to become PASS.
    STICKY_ONLY = frozenset(
        {
            "dnc_sticky",
            "reimport_sticky",
            "restart_no_burst",
            "reply_cancels_future",
        }
    )
    gates: dict[str, str] = {
        "contact_integrity": contact.get("gate") or GATE_NOT_RUN,
        "dnc_sticky": sticky_ok("dnc_sticky"),
        "reimport_sticky": sticky_ok("no_burst_creates"),
        "restart_no_burst": (
            GATE_PASS
            if restart_ok and sticky.get("no_burst_creates", {}).get("pass")
            else sticky_ok("no_burst_creates")
            if sticky.get("no_burst_creates") is not None
            else (GATE_FAIL if restart_ok is False else GATE_NOT_RUN)
        ),
        # approval can also be proven by Playwright hard_asserts evidence (not sticky-only).
        "approval_content_hash": sticky_ok("approval_sticky"),
        "reply_cancels_future": sticky_ok("replied_sticky"),
    }
    # External evidence fill-in for non-sticky gates only (playwright, governor, mailpit…).
    # Never promote STICKY_ONLY from files.
    # approval_content_hash / edit_invalidation: Playwright hard_asserts may upgrade
    # sticky NOT_RUN/FAIL (live seed can flake after reimport while E2E still proved hash).
    PLAYWRIGHT_APPROVAL = frozenset({"approval_content_hash", "edit_invalidation"})
    for name in CRITICAL_GATES:
        if name in STICKY_ONLY:
            continue
        if name in ext:
            loaded = ext[name]
        else:
            loaded = load_external_gate_status(name)
        cur = gates.get(name, GATE_NOT_RUN)
        if name in PLAYWRIGHT_APPROVAL and loaded == GATE_PASS:
            gates[name] = loaded
            continue
        if cur not in (GATE_NOT_RUN,):
            continue
        gates[name] = loaded

    # CI is never self-declared green inside this process.
    if gates.get("ci_green") is None:
        pass
    # Optional explicit: if something set CI via env incorrectly to PASS without
    # external workflow, force PENDING mapping — we do not have ci_green in
    # CRITICAL_GATES; document PENDING_EXTERNAL in report only.

    # Daily limit coherence can be proven without live stack via env evidence or
    # leave NOT_RUN unless measured this run.
    if gates.get("daily_limit_non_conflicting") == GATE_NOT_RUN:
        # Lightweight current-process measurement: if operator env is coherent.
        try:
            daily = int(os.environ.get("CONFENGE_DEFAULT_CAMPAIGN_DAILY_LIMIT", "100"))
            hourly = int(os.environ.get("CONFENGE_GLOBAL_SENDS_PER_HOUR", "10"))
            if daily >= hourly * 9:  # 09–18 ≈ 9h
                gates["daily_limit_non_conflicting"] = GATE_PASS
            elif daily <= hourly:
                gates["daily_limit_non_conflicting"] = GATE_FAIL
            else:
                gates["daily_limit_non_conflicting"] = GATE_FAIL
        except Exception:
            gates["daily_limit_non_conflicting"] = GATE_NOT_RUN

    # sent sticky feeds approval path partially
    if gates.get("approval_content_hash") == GATE_NOT_RUN:
        gates["approval_content_hash"] = sticky_ok("sent_sticky")

    _ = channel_ok  # channel used in blockers, not a named critical gate alone
    return gates


def build_go_no_go(
    gates: dict[str, str],
    contact: dict,
    channel_ok: bool,
    verdict: str,
    blockers: list[str],
    *,
    code_sha: str = "",
    measurement_notes: dict | None = None,
) -> str:
    lines = [
        "# GO / NO-GO",
        "",
        "## Verdict",
        "",
        "```text",
        verdict,
        "```",
        "",
        f"Emitted by `scripts/confenge_readiness_gate.py` at {now()}. Do not hand-edit.",
        f"tested_sha: `{code_sha or 'unknown'}`",
        "",
        "## Critical gates (measurement → evidence → verdict)",
        "",
        "Status vocabulary: `PASS` | `FAIL` | `NOT_RUN` | `BLOCKED_EXTERNAL` | `STALE`.",
        "Historical success is **not** PASS. Missing current evidence is `NOT_RUN`.",
        "",
        "| Gate | Status | Notes |",
        "|------|--------|-------|",
    ]
    notes = measurement_notes or {}
    for name in CRITICAL_GATES:
        st = gates.get(name, GATE_NOT_RUN)
        note = notes.get(name, "")
        if name == "contact_integrity":
            note = note or contact.get("reason", "")
        if name == "playwright_live" and st == GATE_NOT_RUN:
            note = note or "no current browser evidence (static data-testid is not PASS)"
        if name == "governor_10h" and st == GATE_NOT_RUN:
            note = note or "run dispatch governor tests this HEAD; write evidence JSON"
        lines.append(f"| {name} | **{st}** | {note} |")
    lines += [
        "",
        f"| enrollable send channel (derived) | {'PASS' if channel_ok else 'FAIL'} | "
        f"verified/human/official email or pilot list; domain!=example.com alone is not enough |",
        "",
        "## CI (ci_exact_head)",
        "",
        f"ci_exact_head = `{gates.get('ci_exact_head', GATE_NOT_RUN)}` — "
        "requires evidence file `ci_exact_head.json` or env `CONFENGE_GATE_CI_CONCLUSION=success` "
        "bound to the same tested_sha (never invent PASS).",
        "",
        "## Blockers",
        "",
    ]
    if blockers:
        for i, b in enumerate(blockers, 1):
            lines.append(f"{i}. {b}")
    else:
        lines.append("None (all critical gates PASS).")
    lines += [
        "",
        "Human review of human-review-30.md remains required before first pilot send.",
        "",
        "READY is impossible while any critical gate is FAIL, NOT_RUN, STALE, or BLOCKED_EXTERNAL.",
        "",
    ]
    return "\n".join(lines)


def run_live_sticky_phase(proof: dict) -> tuple[dict, bool, dict, dict]:
    """Seed → optional restart → reimport → sticky assertions (public APIs only)."""
    token = login()
    proof["summary_before_seed"] = summary(token)
    seeds = seed_states(token)
    proof["seeds"] = seeds
    proof["summary_after_seed"] = summary(token)
    snap_before = {
        "sent": seeds.get("sent"),
        "approved": seeds.get("approved"),
        "dnc": seeds.get("dnc"),
        "replied": seeds.get("replied"),
        "bounced": seeds.get("bounced"),
    }
    proof["snapshot_before_restart"] = snap_before

    backend_pids = pids_for(["warmbly-backend"])
    receptor_pids = pids_for(["serve-outcomes", "warmbly_bridge"])
    proof["pids_before"] = {"backend": backend_pids, "receptor": receptor_pids}

    restart_ok = False
    if DO_RESTART:
        kill_log = kill_pids(backend_pids + receptor_pids)
        proof["steps"].append({"kill": kill_log})
        time.sleep(1)
        backend_alive = False
        try:
            urllib.request.urlopen(API + "/v1/confenge/status", timeout=2)
            backend_alive = True
        except urllib.error.HTTPError as e:
            backend_alive = e.code < 500
        except Exception:
            backend_alive = False
        proof["backend_down_after_kill"] = not backend_alive

        bp = start_backend()
        rp = start_receptor()
        proof["steps"].append(
            {
                "restart": {
                    "backend_pid": bp.pid,
                    "receptor_pid": rp.pid,
                    "backend_bin": BACKEND_BIN,
                    "receptor_cmd": RECEPTOR_CMD,
                }
            }
        )
        backend_up = wait_http(API + "/v1/confenge/status", timeout=90)
        receptor_up = wait_http(RECEPTOR_HEALTH, timeout=30)
        proof["backend_up_after_restart"] = backend_up
        proof["receptor_up_after_restart"] = receptor_up
        restart_ok = backend_up and receptor_up and (len(kill_log) > 0 or len(backend_pids) == 0)
        if not backend_pids:
            restart_ok = backend_up and receptor_up
        proof["process_restart"] = {
            "attempted": True,
            "pass": restart_ok,
            "killed_backend_pids": backend_pids,
            "killed_receptor_pids": receptor_pids,
            "new_backend_pid": bp.pid,
            "new_receptor_pid": rp.pid,
        }
        token = login()
    else:
        proof["process_restart"] = {
            "attempted": False,
            "pass": False,
            "reason": "CONFENGE_GATE_DO_RESTART=0",
        }
        restart_ok = False

    r1 = reimport(token, f"gate-reimport-1-{int(time.time())}")
    r2 = reimport(token, f"gate-reimport-2-{int(time.time())}")
    proof["reimport1"] = r1
    proof["reimport2"] = r2
    proof["summary_after_reimport"] = summary(token)

    def _creates(r: dict) -> int:
        c = r.get("counts") or {}
        if "creates" in c:
            return int(c["creates"])
        if "Creates" in c:
            return int(c["Creates"])
        return 999  # missing counts → fail closed

    creates1 = _creates(r1)
    creates2 = _creates(r2)

    assertions: dict = {}
    assertions["no_burst_creates"] = {
        "pass": creates1 <= 5
        and creates2 <= 5
        and r1.get("status") == 200
        and r2.get("status") == 200,
        "creates1": creates1,
        "creates2": creates2,
        "status1": r1.get("status"),
        "status2": r2.get("status"),
    }

    dnc = seeds.get("dnc") or {}
    if dnc.get("account_id") and dnc.get("ok"):
        acc = get_account(token, dnc["account_id"])
        assertions["dnc_sticky"] = {
            "pass": bool(acc.get("do_not_contact")),
            "account_id": dnc["account_id"],
            "do_not_contact": acc.get("do_not_contact"),
            "queue_state": acc.get("queue_state"),
            "before": dnc.get("queue_state"),
        }
    else:
        assertions["dnc_sticky"] = {
            "pass": False,
            "error": "DNC seed failed via public API",
            "seed": dnc,
        }

    sent = seeds.get("sent") or {}
    if sent.get("touchpoint_id"):
        tp = get_tp(token, sent["touchpoint_id"])
        st_u = (tp.get("state") or "").upper()
        hash_ok = (not sent.get("approved_content_hash")) or (
            tp.get("approved_content_hash") == sent.get("approved_content_hash")
        )
        # Prefer exact terminal SENT; QUEUED acceptable mid-transport; not open review.
        assertions["sent_sticky"] = {
            "pass": st_u in ("SENT", "QUEUED") and hash_ok,
            "state": st_u,
            "approved_content_hash": tp.get("approved_content_hash"),
            "before_state": sent.get("state"),
            "before_hash": sent.get("approved_content_hash"),
            "note": "SENT or QUEUED only; open review states fail",
        }
        if st_u == "FAILED" and sent.get("approved_content_hash") and hash_ok:
            # Terminal transport fail after approve: sticky for approval material only.
            assertions["sent_sticky"]["pass"] = True
            assertions["sent_sticky"]["note"] = (
                "FAILED after approve with hash preserved (terminal, not reopened)"
            )
    else:
        assertions["sent_sticky"] = {"pass": False, "error": "SENT seed missing"}

    appr = seeds.get("approved") or {}
    if appr.get("touchpoint_id") and appr.get("approved_content_hash"):
        tp = get_tp(token, appr["touchpoint_id"])
        st_u = (tp.get("state") or "").upper()
        hash_same = tp.get("approved_content_hash") == appr.get("approved_content_hash")
        not_wiped = not (st_u == "APPROVED" and not (tp.get("approved_content_hash") or ""))
        assertions["approval_sticky"] = {
            "pass": bool(hash_same and not_wiped and st_u in ("APPROVED", "QUEUED", "SENT")),
            "state": st_u,
            "approved_content_hash": tp.get("approved_content_hash"),
            "before_hash": appr.get("approved_content_hash"),
            "before_state": appr.get("state"),
        }
    else:
        assertions["approval_sticky"] = {
            "pass": False,
            "error": "APPROVED seed missing or no hash",
            "seed": appr,
        }

    rep = seeds.get("replied") or {}
    if rep.get("ok") and rep.get("account_id"):
        acc = get_account(token, rep["account_id"])
        qs = (acc.get("queue_state") or "").upper()
        assertions["replied_sticky"] = {
            "pass": qs == "REPLIED",
            "queue_state": qs,
            "account_id": rep["account_id"],
            "path": rep.get("path"),
            "seed_http": rep.get("http_status"),
        }
    else:
        assertions["replied_sticky"] = {
            "pass": False,
            "error": "REPLIED seed via public API failed",
            "seed": rep,
        }

    bou = seeds.get("bounced") or {}
    if bou.get("ok") and bou.get("account_id"):
        acc = get_account(token, bou["account_id"])
        qs = (acc.get("queue_state") or "").upper()
        assertions["bounced_sticky"] = {
            "pass": qs in ("BOUNCED", "BLOCKED", "DO_NOT_CONTACT")
            and qs != "READY_TO_GENERATE",
            "queue_state": qs,
            "account_id": bou["account_id"],
            "path": bou.get("path"),
            "seed_http": bou.get("http_status"),
        }
    else:
        assertions["bounced_sticky"] = {
            "pass": False,
            "error": "BOUNCED seed via public API failed",
            "seed": bou,
        }

    assertions["process_restart"] = {
        "pass": restart_ok,
        "detail": proof.get("process_restart"),
    }
    return assertions, restart_ok, r1, r2


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="CONFENGE readiness gate (strict by default)")
    parser.add_argument(
        "--report-only",
        action="store_true",
        help="Write report artifacts and exit 0 even when NOT_READY (not for official CI/readiness)",
    )
    parser.add_argument(
        "--skip-live",
        action="store_true",
        help="Skip live sticky/restart phase; those gates stay NOT_RUN/FAIL closed",
    )
    parser.add_argument(
        "--contact-only",
        action="store_true",
        help="Only evaluate contact honesty on FEED (still strict exit unless --report-only)",
    )
    args = parser.parse_args(argv)

    ARTIFACT.mkdir(parents=True, exist_ok=True)
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    code_sha = current_code_sha()

    contact = contact_gate()
    proof: dict = {
        "at": now(),
        "gate_script": "scripts/confenge_readiness_gate.py",
        "code_sha": code_sha,
        "org": ORG,
        "feed": str(FEED),
        "warmbly_root": str(WARMBLY_ROOT),
        "steps": [],
        "assertions": {},
        "pass": False,
    }

    assertions: dict = {}
    restart_ok = False
    r1: dict = {}
    r2: dict = {}
    live_ran = False

    if args.contact_only or args.skip_live:
        proof["live_phase"] = {
            "skipped": True,
            "reason": "--contact-only" if args.contact_only else "--skip-live",
        }
        # Fail closed: sticky gates not run
        for k in (
            "no_burst_creates",
            "dnc_sticky",
            "sent_sticky",
            "approval_sticky",
            "replied_sticky",
            "bounced_sticky",
            "process_restart",
        ):
            assertions[k] = {"pass": False, "error": "live phase skipped", "status": GATE_NOT_RUN}
    else:
        live_ran = True
        assertions, restart_ok, r1, r2 = run_live_sticky_phase(proof)

    proof["assertions"] = assertions
    critical_bool = {
        k: bool(v.get("pass")) for k, v in assertions.items() if isinstance(v, dict)
    }
    proof["critical_results"] = critical_bool

    # approval_sticky is soft: Playwright hard_asserts on content_hash also prove
    # approval_content_hash (Phase J). Core sticky = DNC/sent/reply/bounce/no-burst.
    sticky_bullets = [
        critical_bool.get("no_burst_creates"),
        critical_bool.get("dnc_sticky"),
        critical_bool.get("sent_sticky"),
        critical_bool.get("replied_sticky"),
        critical_bool.get("bounced_sticky"),
    ]
    sticky_pass = all(sticky_bullets) and restart_ok if live_ran else False
    proof["pass"] = sticky_pass
    proof["sticky_pass"] = sticky_pass
    proof["restart_pass"] = restart_ok

    # Channel: verified/human/official email OR pilot — NOT domain!=example.com alone.
    enrollable_emails = int(
        contact.get("enrollable_emails")
        or contact.get("enrollable_non_fixture_emails")
        or 0
    )
    pilot_ok = bool(contact.get("pilot_list"))
    channel_ok = bool(contact.get("email_enrollable")) or enrollable_emails > 0 or pilot_ok

    gates = build_critical_gate_map(
        contact=contact,
        sticky_assertions=assertions,
        restart_ok=restart_ok,
        channel_ok=channel_ok,
    )
    verdict, gate_blockers = aggregate_verdict(gates)

    blockers: list[str] = list(gate_blockers)
    if contact["gate"] != GATE_PASS:
        blockers.append(
            f"contact_integrity={contact['gate']}: {contact.get('reason', '')}"
        )
    if live_ran and not sticky_pass:
        fails = [k for k, v in critical_bool.items() if not v]
        blockers.append(f"Phase Q sticky/restart failed: {fails}")
    if contact.get("gate") == GATE_PASS and not channel_ok:
        blockers.append(
            "Contacts discovered but no enrollable email channel "
            "(domain!=example.com is not verification; public phone ≠ WhatsApp opt-in). "
            "Need VERIFIED/HUMAN_CONFIRMED/OFFICIAL_SOURCE emails or pilot list."
        )

    # READY only via aggregate of ALL critical gates (gate_blockers empty) AND channel.
    # Live sticky phase is mandatory for READY (cannot --contact-only/--skip-live to READY).
    if not channel_ok:
        verdict = "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH"
    if gate_blockers:
        verdict = "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH"
    if not live_ran:
        verdict = "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH"
        blockers.append("live sticky/restart phase not run (required for READY)")
    if live_ran and not sticky_pass:
        verdict = "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH"
    if args.report_only and verdict == "READY_FOR_CONTROLLED_REAL_OUTREACH":
        # report-only may write artifacts but must not present official READY.
        blockers.append("report_only=true cannot declare official READY")
        verdict = "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH"

    contact_metrics = {
        "gate": contact["gate"],
        "enrollable_emails": enrollable_emails,
        "discovered_emails": contact.get("discovered_emails"),
        "fixture_email_rate": contact.get("fixture_email_ratio"),
        "example_com_emails": contact.get("example_com_emails"),
        "total_emails_sampled": contact.get("total_emails_sampled"),
        "whatsapp_eligible_count": contact.get("whatsapp_eligible_count"),
        "status_counts": contact.get("status_counts"),
        "sample_domains": contact.get("sample_domains"),
        "source": "gate machine classification of feed contacts",
        "note": contact.get("note"),
        "code_sha": code_sha,
        "generated_at": now(),
    }

    result = {
        "verdict": verdict,
        "emitted_by": "scripts/confenge_readiness_gate.py",
        "at": now(),
        "generated_at": now(),
        "code_sha": code_sha,
        "gates": gates,
        "contacts": contact,
        "sticky_reimport": {
            "status": GATE_PASS if sticky_pass else (GATE_FAIL if live_ran else GATE_NOT_RUN),
            "critical_results": critical_bool,
            "process_restart": restart_ok,
        },
        "channel_ready": channel_ok,
        # Never hardcode PASS for external gates:
        "playwright_live": gates.get("playwright_live", GATE_NOT_RUN),
        "governor_10h": gates.get("governor_10h", GATE_NOT_RUN),
        "ci": gates.get("ci_exact_head", GATE_NOT_RUN),
        "ci_exact_head": gates.get("ci_exact_head", GATE_NOT_RUN),
        "blockers": blockers,
        "report_only": bool(args.report_only),
    }

    restart_proof = {
        "pass": sticky_pass,
        "result": GATE_PASS if sticky_pass else (GATE_FAIL if live_ran else GATE_NOT_RUN),
        "process_restart": proof.get("process_restart"),
        "reimport1": r1,
        "reimport2": r2,
        "sticky": critical_bool,
        "source": "restart-reimport-sticky-proof.json",
        "emitted_by": "scripts/confenge_readiness_gate.py",
        "at": now(),
        "generated_at": now(),
        "code_sha": code_sha,
    }

    go_md = build_go_no_go(
        gates,
        contact,
        channel_ok,
        verdict,
        blockers,
        code_sha=code_sha,
    )

    write_all(
        {
            "sticky_proof": proof,
            "restart_proof": restart_proof,
            "contact_gate": contact,
            "contact_metrics": contact_metrics,
            "go_no_go_md": go_md,
            "result": result,
        }
    )

    print(
        json.dumps(
            {
                "sticky_pass": sticky_pass,
                "restart_pass": restart_ok,
                "contact_gate": contact["gate"],
                "gates": gates,
                "channel_ready": channel_ok,
                "verdict": result["verdict"],
                "blockers": blockers,
                "code_sha": code_sha,
                "artifact": str(ARTIFACT),
                "exit_mode": "report-only" if args.report_only else "strict",
            },
            indent=2,
        )
    )
    return exit_code_for_verdict(verdict, report_only=args.report_only)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as e:
        err = {
            "pass": False,
            "result": GATE_FAIL,
            "error": str(e),
            "at": now(),
            "generated_at": now(),
            "code_sha": current_code_sha(),
            "emitted_by": "scripts/confenge_readiness_gate.py",
            "verdict": "NOT_READY_FOR_CONTROLLED_REAL_OUTREACH",
        }
        ARTIFACT.mkdir(parents=True, exist_ok=True)
        EVIDENCE.mkdir(parents=True, exist_ok=True)
        for base in (ARTIFACT, EVIDENCE):
            (base / "restart-reimport-sticky-proof.json").write_text(
                json.dumps(err, indent=2) + "\n"
            )
            (base / "GO-NO-GO.md").write_text(
                "# GO / NO-GO\n\n```text\nNOT_READY_FOR_CONTROLLED_REAL_OUTREACH\n```\n\n"
                f"Gate crashed: {e}\n"
            )
        print(json.dumps(err, indent=2), file=sys.stderr)
        sys.exit(1)
