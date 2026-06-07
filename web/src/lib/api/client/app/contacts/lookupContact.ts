import type Contact from "@/lib/api/models/app/contacts/Contact";
import Request from "../../Request";

// Resolve a sender address to a contact in the current organization. Returns
// null when the address isn't a known contact (a normal, non-error outcome).
export default async function lookupContact(email: string): Promise<Contact | null> {
    const res = await Request<{ contact: Contact | null }>({
        method: "GET",
        url: `/contacts/lookup?email=${encodeURIComponent(email)}`,
        authorization: true,
    });
    return res?.contact ?? null;
}
