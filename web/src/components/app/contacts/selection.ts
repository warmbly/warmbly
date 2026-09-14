// Contacts' row selection: the shared table-selection state, plus the wire
// shape the contact bulk endpoints take.

import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import type ContactSelection from "@/lib/api/models/app/contacts/ContactSelection";
import type { RowSelection } from "@/lib/helper/rowSelection";

export * from "@/lib/helper/rowSelection";

/** The wire shape every contact bulk endpoint takes. */
export function toRequest(s: RowSelection, filters: SearchContacts): ContactSelection {
    if (!s.all) return { contacts: s.ids };
    return { contacts: [], all: true, filters, exclude: s.excluded };
}
