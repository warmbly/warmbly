export type AuditAction =
    | "create"
    | "update"
    | "delete"
    | "login"
    | "logout"
    | "api_call"
    | "export"
    | "import"
    | "revoke"
    | "connect"
    | "test"
    | "install"
    | "restart"
    | "upgrade"
    | "uninstall"
    | "rotate_keys"
    | "apply"
    | "assign"
    | "system_update"
    | "reboot"
    | "check_releases"
    | string;

export type AuditEntityType =
    | "campaign"
    | "contact"
    | "email_account"
    | "api_key"
    | "sequence"
    | "user"
    | "session"
    | "worker"
    | "aws_credentials"
    | "worker_profile"
    | "release"
    | string;

export default interface AuditLog {
    id: string;
    user_id: string;
    action_date: string;
    action: AuditAction;
    entity_type: AuditEntityType;
    entity_id?: string;
    ip_address: string;
    user_agent: string;
    changes?: Record<string, string>;
    metadata?: Record<string, string>;
    timestamp: string;
}

export interface AuditLogsResult {
    data: AuditLog[];
    pagination: {
        cursor?: string;
        has_more?: boolean;
        next_cursor?: string;
    };
}
