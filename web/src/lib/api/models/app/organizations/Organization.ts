export default interface Organization {
    id: string
    name: string
    avatar?: string
    plan?: string
    // Built-in role id or a custom role's name.
    role: string
    // Caller's effective permission bitmask in this org (custom-role aware).
    permissions?: number
    created_at: Date
}
