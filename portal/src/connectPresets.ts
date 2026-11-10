// Server settings for the mail hosts clients most often bring. Custom leaves
// every field to the client.

export interface MailPreset {
    id: string;
    label: string;
    imap: { host: string; port: number; security: "tls" | "starttls" };
    smtp: { host: string; port: number; security: "tls" | "starttls" };
    /** Shown under the password field: where this host wants an app password. */
    hint?: string;
}

export const MAIL_PRESETS: MailPreset[] = [
    {
        id: "gmail",
        label: "Gmail (app password)",
        imap: { host: "imap.gmail.com", port: 993, security: "tls" },
        smtp: { host: "smtp.gmail.com", port: 465, security: "tls" },
        hint: "Use an app password from your Google account's security settings, not your normal password.",
    },
    {
        id: "office365",
        label: "Microsoft 365 / Outlook",
        imap: { host: "outlook.office365.com", port: 993, security: "tls" },
        smtp: { host: "smtp.office365.com", port: 587, security: "starttls" },
    },
    {
        id: "zoho",
        label: "Zoho Mail",
        imap: { host: "imap.zoho.com", port: 993, security: "tls" },
        smtp: { host: "smtp.zoho.com", port: 465, security: "tls" },
        hint: "Accounts outside the US use their region's host, for example imap.zoho.eu.",
    },
    {
        id: "yahoo",
        label: "Yahoo Mail",
        imap: { host: "imap.mail.yahoo.com", port: 993, security: "tls" },
        smtp: { host: "smtp.mail.yahoo.com", port: 465, security: "tls" },
        hint: "Yahoo needs an app password generated in your account security settings.",
    },
    {
        id: "icloud",
        label: "iCloud Mail",
        imap: { host: "imap.mail.me.com", port: 993, security: "tls" },
        smtp: { host: "smtp.mail.me.com", port: 587, security: "starttls" },
        hint: "iCloud needs an app-specific password from your Apple account.",
    },
    {
        id: "fastmail",
        label: "Fastmail",
        imap: { host: "imap.fastmail.com", port: 993, security: "tls" },
        smtp: { host: "smtp.fastmail.com", port: 465, security: "tls" },
    },
    {
        id: "godaddy",
        label: "GoDaddy Workspace",
        imap: { host: "imap.secureserver.net", port: 993, security: "tls" },
        smtp: { host: "smtpout.secureserver.net", port: 465, security: "tls" },
    },
];

export const CUSTOM_PRESET = "custom";
