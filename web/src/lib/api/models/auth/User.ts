import type Tag from "../app/Tag";
import type Category from "../app/Category";
import type Folder from "../app/Folder";

export default interface User {
    id: string;

    first_name: string;
    last_name: string;
    email: string;
    avatar_url?: string | null;

    referral_source: string;
    onboarding_completed_at: Date | null;
    // When the product tour was finished or skipped; null offers it on the next visit.
    product_tour_completed_at?: Date | null;

    // Undo-send window in seconds (5..120). A stale server cache may omit
    // it briefly, so readers treat 0/undefined as the default 30.
    undo_send_seconds?: number;

    // Platform admin access. is_admin is true for anyone holding any admin
    // permission; the dashboard only uses it to link to the admin panel.
    is_admin?: boolean;
    admin_permissions?: number;
    // Whether this session presented a second factor. Admin routes refuse a
    // session that did not, so admin actions in the dashboard gate on it too.
    session_mfa_verified?: boolean;

    tags: Tag[];
    categories: Category[];
    folders: Folder[];
    roles: string[];

    updated_at: Date;
    created_at: Date;
}
