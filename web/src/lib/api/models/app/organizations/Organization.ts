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
    // Inbox agent opt-in (manage_settings, paid). When on, an inbound human
    // reply gets an AI-drafted suggested reply awaiting review in the unibox.
    inbox_agent_enabled?: boolean
    // Workspace-shared assistant history (manage_settings). When on, every
    // member with the Use AI permission sees and can continue every
    // assistant conversation in the workspace.
    assistant_shared_history?: boolean
}
