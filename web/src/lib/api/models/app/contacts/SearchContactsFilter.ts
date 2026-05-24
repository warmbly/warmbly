import type { SearchContactsFilterType } from "./search-contacts.types";

export default interface SearchContactsFilter {
    name: string;
    value: string;
    type: SearchContactsFilterType;
}
