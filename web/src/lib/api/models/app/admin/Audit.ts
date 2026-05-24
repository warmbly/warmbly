export interface AdminAuditLog {
    id: string;
    admin_user_id: string;
    action: string;
    target_type: string;
    target_id: string;
    details?: Record<string, unknown>;
    ip_address: string;
    user_agent: string;
    created_at: string;
    admin_user?: {
        id: string;
        email: string;
        first_name?: string;
        last_name?: string;
    };
}

export interface AdminAuditLogSearch {
    admin_user_id?: string;
    action?: string;
    target_type?: string;
    target_id?: string;
    start_date?: string;
    end_date?: string;
    cursor?: string;
    limit?: number;
}

export interface AdminAuditLogsResult {
    data: AdminAuditLog[];
    pagination: {
        cursor?: string;
        has_more?: boolean;
    };
}
