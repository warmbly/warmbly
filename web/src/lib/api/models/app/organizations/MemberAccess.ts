// Mirror of internal/models/member_access.go. A member's role says what they
// may do; their access scope says which resources it applies to.

export type AccessScope = "workspace" | "restricted";

export default interface MemberAccess {
    scope: AccessScope;
    // Folder grants follow the folder's contents at the time of each request.
    campaign_folder_ids: string[];
    campaign_ids: string[];
    email_account_ids: string[];
}

export interface SuggestedSender {
    id: string;
    email: string;
    name: string;
    campaign_ids: string[];
}

export const WORKSPACE_ACCESS: MemberAccess = {
    scope: "workspace",
    campaign_folder_ids: [],
    campaign_ids: [],
    email_account_ids: [],
};
