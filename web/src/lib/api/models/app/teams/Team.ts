// A team groups organization members under a named, colored label.
// Org-scoped. Mirrors the backend Team JSON shape; `members` is always
// present (never null) and hydrated on detail/mutation responses.
export interface TeamMember {
    user_id: string;
    email: string;
    name: string;
    added_at: string;
}

export default interface Team {
    id: string;
    organization_id: string;
    name: string;
    color: string;
    created_at: string;
    updated_at: string;
    members: TeamMember[];
}
