// Display names for a mailbox's host and sign-in method, and the one-line
// label the mailbox list and drawer show ("Google Workspace · App password").
import type { MailAuthMethod, MailHost } from "@/lib/api/models/app/emails/MailboxImport";

export const MAIL_HOST_LABELS: Record<Exclude<MailHost, "">, string> = {
    google_workspace: "Google Workspace",
    gmail: "Gmail",
    microsoft365: "Microsoft 365",
    outlook: "Outlook.com",
    zoho: "Zoho Mail",
    yahoo: "Yahoo Mail",
    aol: "AOL Mail",
    icloud: "iCloud Mail",
    fastmail: "Fastmail",
    godaddy: "GoDaddy",
    namecheap: "Namecheap",
    ionos: "IONOS",
    hostinger: "Hostinger",
    ovh: "OVHcloud",
    migadu: "Migadu",
    purelymail: "Purelymail",
    rackspace: "Rackspace",
    yandex: "Yandex Mail",
    gmx: "GMX",
    proton: "Proton Mail",
    other: "Other host",
};

export const AUTH_METHOD_LABELS: Record<Exclude<MailAuthMethod, "">, string> = {
    password: "Password",
    app_password: "App password",
    oauth: "OAuth",
    delegated: "Admin grant",
};

// The connect type the mailbox list showed before hosts were detected.
const PROVIDER_LABELS: Record<string, string> = {
    gmail: "Gmail",
    outlook: "Outlook",
    smtp_imap: "SMTP/IMAP",
};

export function mailHostLabel(host?: string | null): string {
    if (!host) return "";
    return MAIL_HOST_LABELS[host as Exclude<MailHost, "">] ?? host;
}

export function authMethodLabel(method?: string | null): string {
    if (!method) return "";
    return AUTH_METHOD_LABELS[method as Exclude<MailAuthMethod, "">] ?? method;
}

/** Which of the two logos the app ships a host maps to, if any. */
export function mailHostLogo(host?: string | null): "google" | "microsoft" | null {
    if (host === "google_workspace" || host === "gmail") return "google";
    if (host === "microsoft365" || host === "outlook") return "microsoft";
    return null;
}

/** A consumer Gmail address, which connects with an app password rather than Google sign-in. */
export function isPersonalGmail(address?: string | null): boolean {
    const a = (address ?? "").trim();
    const at = a.lastIndexOf("@");
    if (at < 0) return false;
    const domain = a.slice(at + 1).toLowerCase().replace(/\.$/, "");
    return domain === "gmail.com" || domain === "googlemail.com";
}

/** The OAuth provider a host signs in with, when it has one. */
export function mailHostOAuthProvider(host?: string | null): "gmail" | "outlook" | null {
    const logo = mailHostLogo(host);
    return logo === "google" ? "gmail" : logo === "microsoft" ? "outlook" : null;
}

/** "Google Workspace · App password"; falls back to the connect type when the host is unknown. */
export function mailboxConnectionLabel(box: { mail_host?: string | null; auth_method?: string | null; provider?: string | null }): string {
    const host = mailHostLabel(box.mail_host);
    if (!host) return PROVIDER_LABELS[box.provider ?? ""] ?? (box.provider ?? "").replace("_", "/");
    const method = authMethodLabel(box.auth_method);
    return method ? `${host} · ${method}` : host;
}
