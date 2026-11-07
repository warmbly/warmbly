import type BulkEditContactsField from "./BulkEditContactsField";
import type ContactSelection from "./ContactSelection";

export default interface BulkEditContacts extends ContactSelection {
    add_campaigns: string[];
    remove_campaigns: string[];
    // Leave out of add_campaigns anyone whose sequence runs in another campaign.
    skip_active_in_other_campaigns?: boolean;
    add_categories?: string[];
    remove_categories?: string[];
    fields: BulkEditContactsField[];
    subscribe?: boolean;
}
