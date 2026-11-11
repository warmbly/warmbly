// The connect page's vendor route: a client who bought mailboxes from an inbox vendor names the
// account once, picks the mailboxes, and they are connected for them. The key is sent a single
// time and never comes back to this page: the pick refers to it by a short-lived ticket.

import type React from "react";
import { useMemo, useState } from "react";

import {
    ConnectError,
    type ConnectLink,
    importVendorMailboxes,
    listVendorMailboxes,
    type VendorImportStarted,
    type VendorMailboxes,
} from "./connectApi";
import { BackIcon, CheckIcon, ChevronIcon } from "./icons";
import { Turnstile } from "./Turnstile";
import { resetTurnstile } from "./turnstileScript";

const PROVIDER: Record<string, string> = { google: "Google", microsoft: "Microsoft", smtp: "IMAP and SMTP" };

export function VendorForm({
    token,
    link,
    errorCopy,
    onBack,
    onStarted,
}: {
    token: string;
    link: ConnectLink;
    errorCopy: (err: unknown) => string;
    onBack: () => void;
    onStarted: (started: VendorImportStarted) => void;
}) {
    const vendors = link.vendors ?? [];
    const [vendorId, setVendorId] = useState(vendors[0]?.id ?? "");
    const [fields, setFields] = useState<Record<string, string>>({});
    const [captcha, setCaptcha] = useState("");
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [found, setFound] = useState<VendorMailboxes | null>(null);
    const [picked, setPicked] = useState<Set<string>>(new Set());
    const [started, setStarted] = useState<VendorImportStarted | null>(null);

    const vendor = vendors.find((v) => v.id === vendorId);
    const open = useMemo(() => (found?.mailboxes ?? []).filter((m) => !m.connected), [found]);
    const room = found ? Math.max(0, found.remaining) : 0;

    const find = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!vendor) return;
        if (link.captcha_site_key && !captcha) {
            setError("Complete the security check first.");
            return;
        }
        setBusy(true);
        setError(null);
        try {
            const out = await listVendorMailboxes(token, { vendor: vendor.id, fields, captcha_token: captcha || undefined });
            // The key has done its job here; the ticket stands in for it from now on.
            setFields({});
            setFound(out);
            const fits = out.mailboxes.filter((m) => !m.connected).slice(0, Math.max(0, out.remaining));
            setPicked(new Set(fits.map((m) => m.id)));
        } catch (err) {
            setError(errorCopy(err));
            setCaptcha("");
            resetTurnstile();
        } finally {
            setBusy(false);
        }
    };

    const startOver = () => {
        setFound(null);
        setPicked(new Set());
        setCaptcha("");
        setError(null);
    };

    const connect = async () => {
        if (!found || picked.size === 0) return;
        setBusy(true);
        setError(null);
        try {
            const out = await importVendorMailboxes(token, found.ticket, [...picked]);
            setStarted(out);
            onStarted(out);
        } catch (err) {
            setError(errorCopy(err));
            // A ticket is good once and for a few minutes; after that the key is asked for again.
            if (err instanceof ConnectError && err.code === "mailbox_vendor_hold_expired") startOver();
        } finally {
            setBusy(false);
        }
    };

    const toggle = (id: string) =>
        setPicked((cur) => {
            const next = new Set(cur);
            if (next.has(id)) next.delete(id);
            else if (next.size < room) next.add(id);
            return next;
        });

    if (started) {
        return (
            <div className="cx-state">
                <span className="cx-state-icon cx-check" aria-hidden>
                    <CheckIcon />
                </span>
                <h1>{started.mailboxes === 1 ? "Your mailbox is being connected" : `Your ${started.mailboxes} mailboxes are being connected`}</h1>
                <p>
                    This takes a minute or two and finishes on its own. {link.workspace} is told if any of them needs something from you. You can
                    close this page.
                </p>
                <button type="button" className="cx-link" onClick={onBack}>
                    <BackIcon /> Connect more
                </button>
            </div>
        );
    }

    if (found) {
        return (
            <div className="cx-form">
                <button type="button" className="cx-link cx-back" onClick={startOver} disabled={busy}>
                    <BackIcon /> Use a different account
                </button>
                <h1 className="cx-title">Pick the mailboxes to connect</h1>
                <p className="cx-sub">
                    {open.length === 0
                        ? `Every mailbox in this account is already connected to ${link.workspace}.`
                        : `${picked.size} of ${open.length} selected. This link connects up to ${room} more.`}
                </p>
                {open.length > 0 && (
                    <div className="cx-pick-tools">
                        <button type="button" className="cx-link" disabled={busy} onClick={() => setPicked(new Set(open.slice(0, room).map((m) => m.id)))}>
                            {open.length > room ? `Select the first ${room}` : "Select all"}
                        </button>
                        <button type="button" className="cx-link" disabled={busy || picked.size === 0} onClick={() => setPicked(new Set())}>
                            Clear
                        </button>
                    </div>
                )}
                <ul className="cx-pick">
                    {found.mailboxes.map((m) => {
                        const on = picked.has(m.id);
                        const full = !on && picked.size >= room;
                        return (
                            <li key={m.id}>
                                <label className={m.connected || full ? "cx-pick-off" : undefined}>
                                    <input type="checkbox" checked={on} disabled={busy || m.connected || full} onChange={() => toggle(m.id)} />
                                    <span className="cx-addr">
                                        {m.email}
                                        {m.name && <small>{m.name}</small>}
                                    </span>
                                    <span className="cx-via">{m.connected ? "Already connected" : PROVIDER[m.provider ?? ""] ?? ""}</span>
                                </label>
                            </li>
                        );
                    })}
                </ul>
                {error && (
                    <p className="cx-error" role="alert">
                        {error}
                    </p>
                )}
                <button type="button" className="cx-primary" disabled={busy || picked.size === 0} onClick={() => void connect()}>
                    {busy ? (
                        <>
                            <span className="cx-spinner cx-spinner-sm" aria-hidden /> Starting…
                        </>
                    ) : picked.size === 1 ? (
                        "Connect 1 mailbox"
                    ) : (
                        `Connect ${picked.size} mailboxes`
                    )}
                </button>
            </div>
        );
    }

    return (
        <form className="cx-form" onSubmit={(e) => void find(e)}>
            <button type="button" className="cx-link cx-back" onClick={onBack} disabled={busy}>
                <BackIcon /> Other ways to connect
            </button>
            <h1 className="cx-title">Connect mailboxes from your inbox vendor</h1>
            <p className="cx-sub">
                Bought your mailboxes from a vendor? Enter the account's API key and pick the ones to connect, all at once. The key is checked with
                the vendor and stored encrypted, so {link.workspace} can keep them connected. It is never shown to anyone.
            </p>
            {vendors.length > 1 && (
                <label className="cx-field">
                    <span>Vendor</span>
                    <span className="cx-select">
                        <select
                            value={vendorId}
                            disabled={busy}
                            onChange={(e) => {
                                setVendorId(e.target.value);
                                setFields({});
                            }}
                        >
                            {vendors.map((v) => (
                                <option key={v.id} value={v.id}>
                                    {v.label}
                                </option>
                            ))}
                        </select>
                        <ChevronIcon />
                    </span>
                </label>
            )}
            {vendor?.fields.map((f) => (
                <label key={f.key} className="cx-field">
                    <span>
                        {vendors.length === 1 ? `${vendor.label} ` : ""}
                        {f.label}
                    </span>
                    <input
                        type={f.secret ? "password" : "text"}
                        required={f.required}
                        autoComplete="off"
                        value={fields[f.key] ?? ""}
                        onChange={(e) => setFields((cur) => ({ ...cur, [f.key]: e.target.value }))}
                        disabled={busy}
                    />
                    {f.help && <small>{f.help}</small>}
                </label>
            ))}
            {vendor?.key_help_url && (
                <p className="cx-note cx-note-left">
                    <a href={vendor.key_help_url} target="_blank" rel="noreferrer">
                        Where to find the {vendor.label} key
                    </a>
                </p>
            )}
            {link.captcha_site_key && <Turnstile siteKey={link.captcha_site_key} onToken={setCaptcha} theme={link.brand.theme.appearance} />}
            {error && (
                <p className="cx-error" role="alert">
                    {error}
                </p>
            )}
            <button type="submit" className="cx-primary" disabled={busy || !vendor}>
                {busy ? (
                    <>
                        <span className="cx-spinner cx-spinner-sm" aria-hidden /> Asking {vendor?.label ?? "the vendor"}…
                    </>
                ) : (
                    "Find my mailboxes"
                )}
            </button>
        </form>
    );
}
