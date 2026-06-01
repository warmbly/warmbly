export type AuditAction =
    | "create"
    | "update"
    | "delete"
    | "api_call"
    | "export"
    | "import"
    | "revoke"
    | "connect"
    | "start"
    | "stop"
    | "pause"
    | "resume"
    | "send"
    | "duplicate"
    | "disconnect"
    | "rotate"
    | "invite"
    | "remove"
    | "transfer"
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
    | "organization"
    | "organization_member"
    | "invitation"
    | "template"
    | "webhook"
    | "integration"
    | "warmup_routing_rule"
    | "folder"
    | "tag"
    | "category"
    | "subscription"
    | "settings"
    | "crm_pipeline"
    | "crm_stage"
    | "crm_deal"
    | "crm_task"
    | "crm_note"
    | "unibox"
    | "worker"
    | "aws_credentials"
    | "worker_profile"
    | "release"
    | string;

// AuditActor is the member who performed the action, resolved from the users
// table so the dashboard can render who did what. Absent if that user was
// since deleted.
export interface AuditActor {
    id: string;
    first_name: string;
    last_name: string;
    email: string;
}

export default interface AuditLog {
    id: string;
    org_id: string;
    user_id: string; // actor id (kept for backwards compatibility)
    actor?: AuditActor;
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
