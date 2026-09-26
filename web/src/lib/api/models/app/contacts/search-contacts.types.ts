// A built-in sort column, or "custom:<key>" to order on a contact custom
// field. Custom fields sort as text with rows lacking the field grouped at the
// end when ascending and the start when descending.
export type SearchContactsSortBy =
    | 'created_at'
    | 'updated_at'
    | 'first_name'
    | 'last_name'
    | 'email'
    | 'company'
    | 'phone'
    | 'campaign_count'
    | 'mail_host'
    | `custom:${string}`;

export type SearchContactsFilterType =
    'equal' | 'starts_with' | 'ends_with' | 'contains';
