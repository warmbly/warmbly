import type { Acquisition } from "@/lib/acquisition";

export default interface Register {
    email: string,
    password: string,
    turnstile: string,
    // Team-invitation token. It is what lets a signup through on an
    // invite_only instance, so it must survive the whole register flow.
    invite?: string
    // Where the signup came from, read from this page's query string. Omitted
    // for a direct visit, and then nothing is stored server-side either.
    acquisition?: Acquisition
}
