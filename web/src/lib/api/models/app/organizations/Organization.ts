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
    // Org-wide team presence privacy (admin-controlled). When false, the
    // realtime service stops broadcasting that signal to teammates.
    presence_show_online?: boolean
    presence_show_activity?: boolean
    // AI voice profile (manage_settings). Grounds every AI writing surface.
    product_description?: string
    icp_notes?: string
    voice_profile?: string
}
