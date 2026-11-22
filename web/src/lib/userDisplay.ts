// How the signed-in person is labelled. The API's email is the address they are shown under, which
// is empty for someone a reseller signs in without giving one: then the name stands alone.
import type User from "@/lib/api/models/auth/User";

type Named = Pick<User, "first_name" | "last_name" | "email">;

export function userDisplayName(user: Named): string {
    const full = [user.first_name, user.last_name].map((s) => s?.trim()).filter(Boolean).join(" ");
    return full || user.email || "Your account";
}

export function userInitials(user: Named): string {
    const first = user.first_name?.trim()?.[0];
    const last = user.last_name?.trim()?.[0];
    if (first) return `${first}${last ?? ""}`.toUpperCase();
    return (user.email || "?").slice(0, 2).toUpperCase();
}
