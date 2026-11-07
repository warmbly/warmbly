import type { SigninMigration } from "@/lib/api/models/app/emails/MailboxSources";

/** What a group of mailboxes moves to: a domain's existing grant, the new Google sign-in, or an app password. */
export type MigrationRoute = "grant" | "signin" | "app_password";

/** canSignIn is whether this instance has a Google sign-in client; without one only an app password can work. */
export function migrationRoute(group: SigninMigration, canSignIn: boolean): MigrationRoute {
    if (group.kind === "personal") return "app_password";
    if (group.grant_id) return "grant";
    return canSignIn ? "signin" : "app_password";
}
