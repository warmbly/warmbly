// Connection security for a mailbox leg. TLS is mandatory either way; the
// mode says whether it is negotiated before the greeting (implicit) or
// upgraded in-band after it (STARTTLS). Mirrors models.MailSecurity* in Go.
export type MailSecurity = "tls" | "starttls";

export default interface Service {
    username: string;
    password: string;
    host: string;
    port: number;
    /** Omitted means "infer from the port", matching the backend. */
    security?: MailSecurity;
}

/** Conventional SMTP default: 465 is implicit TLS, everything else STARTTLS. */
export function defaultSmtpSecurity(port: number): MailSecurity {
    return port === 465 ? "tls" : "starttls";
}

/** Conventional IMAP default: 143 is STARTTLS, everything else implicit TLS. */
export function defaultImapSecurity(port: number): MailSecurity {
    return port === 143 ? "starttls" : "tls";
}

/** Any routable TCP port; the security mode carries how to connect. */
export function validPort(port: number): boolean {
    return Number.isInteger(port) && port > 0 && port <= 65535;
}
