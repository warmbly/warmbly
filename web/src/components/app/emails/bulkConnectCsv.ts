// bulkConnectCsv — reading and writing the CSV the bulk mailbox import uses.
// Pure functions, kept apart from the panel so it stays a component-only file.

import Papa from "papaparse";
import type AddEmail from "@/lib/api/models/app/emails/AddEmail";
import {
    defaultImapSecurity,
    defaultSmtpSecurity,
    validPort,
    type MailSecurity,
} from "@/lib/api/models/app/emails/Service";
import { downloadBlob } from "@/lib/api/client/app/contacts/exportContacts";

export const TEMPLATE_COLUMNS = [
    "email",
    "name",
    "smtp_host",
    "smtp_port",
    "smtp_user",
    "smtp_password",
    "smtp_security",
    "imap_host",
    "imap_port",
    "imap_user",
    "imap_password",
    "imap_security",
] as const;

const TEMPLATE_EXAMPLE = [
    "alex@company.com",
    "Alex Rivera",
    "smtp.company.com",
    "587",
    "alex@company.com",
    "app-password",
    "starttls",
    "imap.company.com",
    "993",
    "alex@company.com",
    "app-password",
    "tls",
];

/** Header spellings we accept, mapped onto the template's names. */
const ALIASES: Record<string, string> = {
    email: "email",
    address: "email",
    email_address: "email",
    name: "name",
    display_name: "name",
    from_name: "name",
    sender_name: "name",
    first_name: "first_name",
    last_name: "last_name",
    smtp_host: "smtp_host",
    smtp_server: "smtp_host",
    smtp_port: "smtp_port",
    smtp_user: "smtp_user",
    smtp_username: "smtp_user",
    smtp_login: "smtp_user",
    smtp_password: "smtp_password",
    smtp_pass: "smtp_password",
    smtp_security: "smtp_security",
    smtp_encryption: "smtp_security",
    imap_host: "imap_host",
    imap_server: "imap_host",
    imap_port: "imap_port",
    imap_user: "imap_user",
    imap_username: "imap_user",
    imap_login: "imap_user",
    imap_password: "imap_password",
    imap_pass: "imap_password",
    imap_security: "imap_security",
    imap_encryption: "imap_security",
    // One password or login for both legs, the usual case.
    password: "password",
    app_password: "password",
    username: "username",
    user: "username",
    login: "username",
};

function normaliseHeader(h: string): string {
    const key = h.trim().toLowerCase().replace(/[\s-]+/g, "_");
    return ALIASES[key] ?? key;
}

function parseSecurity(raw: string | undefined, port: number, leg: "smtp" | "imap"): MailSecurity | null {
    const v = (raw ?? "").trim().toLowerCase();
    if (v === "") return leg === "smtp" ? defaultSmtpSecurity(port) : defaultImapSecurity(port);
    if (v === "tls" || v === "ssl" || v === "ssl/tls" || v === "implicit") return "tls";
    if (v === "starttls" || v === "start_tls" || v === "start-tls") return "starttls";
    return null;
}

function nameFromEmail(email: string): string {
    const local = email.split("@")[0] ?? "";
    return local
        .split(/[._-]+/)
        .filter(Boolean)
        .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
        .join(" ");
}

/** One line of the file after normalisation. */
export interface BulkRow {
    /** 1-based line in the file, counting the header. */
    line: number;
    raw: Record<string, string>;
    account: AddEmail | null;
    /** Why the row cannot be sent at all; set only for invalid rows. */
    problem?: string;
    status: "ready" | "invalid" | "pending" | "connected" | "skipped" | "failed";
    code?: string;
    message?: string;
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function buildRow(line: number, raw: Record<string, string>): BulkRow {
    const get = (k: string) => (raw[k] ?? "").trim();
    const email = get("email");
    const invalid = (problem: string): BulkRow => ({ line, raw, account: null, problem, status: "invalid" });

    if (!email) return invalid("Missing email address");
    if (!EMAIL_RE.test(email)) return invalid("Not a valid email address");

    let name = get("name");
    if (!name) name = [get("first_name"), get("last_name")].filter(Boolean).join(" ");
    if (!name) name = nameFromEmail(email);

    const sharedUser = get("username") || email;
    const sharedPass = get("password");

    const smtpHost = get("smtp_host");
    const imapHost = get("imap_host");
    if (!smtpHost) return invalid("Missing smtp_host");
    if (!imapHost) return invalid("Missing imap_host");

    const smtpPort = Number(get("smtp_port") || "587");
    const imapPort = Number(get("imap_port") || "993");
    if (!validPort(smtpPort)) return invalid("smtp_port must be between 1 and 65535");
    if (!validPort(imapPort)) return invalid("imap_port must be between 1 and 65535");

    const smtpPass = get("smtp_password") || sharedPass;
    const imapPass = get("imap_password") || sharedPass;
    if (!imapPass) return invalid("Missing imap_password (or a shared password column)");
    if (!smtpPass) return invalid("Missing smtp_password (or a shared password column)");

    const smtpSecurity = parseSecurity(raw.smtp_security, smtpPort, "smtp");
    const imapSecurity = parseSecurity(raw.imap_security, imapPort, "imap");
    if (!smtpSecurity) return invalid("smtp_security must be tls or starttls");
    if (!imapSecurity) return invalid("imap_security must be tls or starttls");

    return {
        line,
        raw,
        status: "ready",
        account: {
            email,
            name,
            smtp: {
                host: smtpHost,
                port: smtpPort,
                username: get("smtp_user") || sharedUser,
                password: smtpPass,
                security: smtpSecurity,
            },
            imap: {
                host: imapHost,
                port: imapPort,
                username: get("imap_user") || sharedUser,
                password: imapPass,
                security: imapSecurity,
            },
        },
    };
}

export function parseBulkFile(file: File): Promise<{ rows: BulkRow[]; columns: string[] }> {
    return new Promise((resolve, reject) => {
        Papa.parse<Record<string, string>>(file, {
            header: true,
            skipEmptyLines: "greedy",
            transformHeader: normaliseHeader,
            complete: (res) => {
                const columns = (res.meta.fields ?? []).filter((c) => c !== "");
                if (!columns.includes("email")) {
                    reject(new Error("The file needs an email column. Download the template to see the expected headers."));
                    return;
                }
                const rows = res.data.map((raw, i) => buildRow(i + 2, raw));
                if (rows.length === 0) {
                    reject(new Error("The file has a header but no rows."));
                    return;
                }
                resolve({ rows, columns });
            },
            error: (err) => reject(new Error(err.message || "Could not read the file.")),
        });
    });
}

export function downloadTemplate() {
    const csv = Papa.unparse([TEMPLATE_COLUMNS as unknown as string[], TEMPLATE_EXAMPLE]);
    downloadBlob(new Blob(["﻿" + csv], { type: "text/csv;charset=utf-8" }), "warmbly-mailboxes-template.csv");
}

/** Columns never written to a download: a CSV in a Downloads folder is
 *  synced, backed up and shared far more than the user intends. */
const SECRET_COLUMNS = new Set(["password", "smtp_password", "imap_password"]);

/** The rows that did not connect, with the user's own columns (minus
 *  passwords) plus the reason. */
export function failedRowsCSV(rows: BulkRow[], columns: string[]): string {
    const keep = columns.filter((c) => !SECRET_COLUMNS.has(c));
    const header = [...keep, "error"];
    const body = rows.map((r) => [...keep.map((c) => r.raw[c] ?? ""), r.problem ?? r.message ?? r.code ?? ""]);
    return Papa.unparse([header, ...body]);
}

