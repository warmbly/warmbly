// Shapes returned by /auth/*. Kept in lockstep with web/src/lib/api/models/auth/.

import type { AdminToken } from "@/lib/auth/storage";

export interface LoginRequest {
    email: string;
    password: string;
    captcha?: string;
}

export type LoginResponse = AdminToken;

export interface AdminProfile {
    id: string;
    email: string;
    first_name?: string;
    last_name?: string;
    avatar?: string;
    is_admin?: boolean;
    admin_permissions?: string[];
    [k: string]: unknown;
}
