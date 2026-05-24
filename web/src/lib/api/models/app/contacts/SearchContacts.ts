import type { SearchContactsSortBy } from "./search-contacts.types";
import type SearchContactsFilter from "./SearchContactsFilter";

export default interface SearchContacts {
    query: string;
    filters: SearchContactsFilter[];
    campaign_ids: string[];
    min_campaigns?: number;
    max_campaigns?: number;
    subscribed?: boolean;
    created_after?: Date;
    created_before?: Date;
    updated_after?: Date;
    updated_before?: Date;
    sort_by: SearchContactsSortBy;
    reverse: boolean;
}
