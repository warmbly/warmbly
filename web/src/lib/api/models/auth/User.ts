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

    tags: Tag[];
    categories: Category[];
    folders: Folder[];
    roles: string[];

    updated_at: Date;
    created_at: Date;
}
