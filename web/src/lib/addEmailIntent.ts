// Where the Add account dialog should open, for a caller that knows what the person came to do:
// the agency's mailbox page sends them straight to the vendor import or the file import of a client.

export interface AddEmailIntent {
    view: "pick" | "bulk" | "vendor";
    /** The vendor account the import starts on. */
    vendorConnectionId?: string;
}

let pending: AddEmailIntent | null = null;

/** Set before opening the dialog; it is used once. */
export function setAddEmailIntent(intent: AddEmailIntent) {
    pending = intent;
}

export function takeAddEmailIntent(): AddEmailIntent | null {
    const intent = pending;
    pending = null;
    return intent;
}
