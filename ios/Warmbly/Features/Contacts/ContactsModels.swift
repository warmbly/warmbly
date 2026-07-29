import Foundation

// Contacts wire models, mirroring the Go json tags in
// internal/models/contact.go, crm.go, and group.go exactly.
// All keys are snake_case on the wire; every model declares CodingKeys.

// MARK: - Wire date helpers

/// Go marshals `time.Time` as RFC3339Nano. Keyset params (`before_at`,
/// `before`) must be echoed back verbatim, so cursor-bearing timestamps are
/// kept as raw strings and parsed lazily for display.
enum ContactsWire {
    // ISO8601FormatStyle is Sendable, unlike ISO8601DateFormatter.
    static let isoFractional = Date.ISO8601FormatStyle(includingFractionalSeconds: true)
    static let isoPlain = Date.ISO8601FormatStyle()

    static func date(_ raw: String?) -> Date? {
        guard let raw, !raw.isEmpty else { return nil }
        return (try? isoFractional.parse(raw)) ?? (try? isoPlain.parse(raw))
    }
}

// MARK: - Contact

struct ContactMiniCampaign: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var name: String?

    enum CodingKeys: String, CodingKey {
        case id, name
    }
}

struct ContactMiniCategory: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var title: String?
    var color: String?

    enum CodingKeys: String, CodingKey {
        case id, title, color
    }
}

/// Per-campaign lead progress; present only when the search filtered by
/// exactly one campaign.
struct ContactCampaignLead: Codable, Hashable, Sendable {
    var status: String?
    var sent: Int?
    var opened: Int?
    var clicked: Int?
    var replied: Int?
    var bounced: Int?
    var lastActivityAt: Date?
    var currentStep: String?

    enum CodingKeys: String, CodingKey {
        case status, sent, opened, clicked, replied, bounced
        case lastActivityAt = "last_activity_at"
        case currentStep = "current_step"
    }
}

struct ContactEngagement: Codable, Hashable, Sendable {
    var totalSent: Int?
    var totalOpened: Int?
    var totalClicked: Int?
    var totalReplied: Int?
    var totalBounced: Int?
    var totalComplained: Int?
    var lastSentAt: Date?
    var lastOpenedAt: Date?
    var lastClickedAt: Date?
    var lastRepliedAt: Date?
    var lastBouncedAt: Date?

    enum CodingKeys: String, CodingKey {
        case totalSent = "total_sent"
        case totalOpened = "total_opened"
        case totalClicked = "total_clicked"
        case totalReplied = "total_replied"
        case totalBounced = "total_bounced"
        case totalComplained = "total_complained"
        case lastSentAt = "last_sent_at"
        case lastOpenedAt = "last_opened_at"
        case lastClickedAt = "last_clicked_at"
        case lastRepliedAt = "last_replied_at"
        case lastBouncedAt = "last_bounced_at"
    }
}

struct ContactSuppression: Codable, Hashable, Sendable {
    var reason: String?
    var source: String?
    var expiresAt: Date?
    var createdAt: Date?

    enum CodingKeys: String, CodingKey {
        case reason, source
        case expiresAt = "expires_at"
        case createdAt = "created_at"
    }
}

/// `models.Contact` (search rows) and, with `engagement`/`suppression`
/// hydrated, `models.ContactDetail` (GET /contacts/:id).
struct Contact: Codable, Identifiable, Sendable {
    var id: String
    var firstName: String?
    var lastName: String?
    var email: String?
    var company: String?
    var phone: String?
    var customFields: [String: String]?
    var subscribed: Bool?
    var campaigns: [ContactMiniCampaign]?
    var categories: [ContactMiniCategory]?
    var campaignLead: ContactCampaignLead?
    var updatedAt: Date?
    var createdAt: Date?

    // Present only on GET /contacts/:id.
    var engagement: ContactEngagement?
    var suppression: ContactSuppression?

    enum CodingKeys: String, CodingKey {
        case id, email, company, phone, subscribed, campaigns, categories
        case firstName = "first_name"
        case lastName = "last_name"
        case customFields = "custom_fields"
        case campaignLead = "campaign_lead"
        case updatedAt = "updated_at"
        case createdAt = "created_at"
        case engagement, suppression
    }

    var displayName: String {
        let name = [firstName, lastName]
            .compactMap { $0?.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
            .joined(separator: " ")
        if !name.isEmpty { return name }
        if let email, !email.isEmpty { return email }
        return "Contact"
    }

    /// True when the row has a name, so the email belongs on the second line.
    var hasName: Bool {
        let first = firstName?.trimmingCharacters(in: .whitespaces) ?? ""
        let last = lastName?.trimmingCharacters(in: .whitespaces) ?? ""
        return !(first.isEmpty && last.isEmpty)
    }
}

extension Contact: Hashable {
    static func == (lhs: Contact, rhs: Contact) -> Bool {
        lhs.id == rhs.id && lhs.updatedAt == rhs.updatedAt
    }

    func hash(into hasher: inout Hasher) {
        hasher.combine(id)
        hasher.combine(updatedAt)
    }
}

// MARK: - Sent emails (GET /contacts/:id/emails)

struct ContactEmailActivity: Codable, Identifiable, Sendable {
    var taskID: String
    var status: String?
    var messageID: String?
    var subject: String?
    /// Kept raw: paged back verbatim as `before_at` (RFC3339Nano keyset).
    var sentAtRaw: String?
    var emailAccountID: String?
    var emailAccountEmail: String?
    var emailAccountName: String?
    var campaignID: String?
    var campaignName: String?
    var stepID: String?
    var stepName: String?
    var openedAt: Date?
    var clickedAt: Date?
    var repliedAt: Date?
    var bouncedAt: Date?

    enum CodingKeys: String, CodingKey {
        case taskID = "task_id"
        case status, subject
        case messageID = "message_id"
        case sentAtRaw = "sent_at"
        case emailAccountID = "email_account_id"
        case emailAccountEmail = "email_account_email"
        case emailAccountName = "email_account_name"
        case campaignID = "campaign_id"
        case campaignName = "campaign_name"
        case stepID = "step_id"
        case stepName = "step_name"
        case openedAt = "opened_at"
        case clickedAt = "clicked_at"
        case repliedAt = "replied_at"
        case bouncedAt = "bounced_at"
    }

    var id: String { taskID }
    var sentAt: Date? { ContactsWire.date(sentAtRaw) }
}

// MARK: - Timeline (GET /contacts/:id/timeline)

struct ContactTimelineEntry: Codable, Sendable {
    var type: String?
    /// Kept raw: the oldest entry's `at` is echoed back as `before`.
    var atRaw: String?
    var emailAccountID: String?
    var emailAccountEmail: String?
    var emailAccountName: String?
    var campaignID: String?
    var campaignName: String?
    var stepID: String?
    var stepName: String?
    var taskID: String?
    var subject: String?
    var reason: String?
    var source: String?
    var provider: String?
    var intent: String?
    var content: String?
    var scheduledFor: Date?
    var joinURL: String?
    var meetingState: String?
    var userID: String?

    enum CodingKeys: String, CodingKey {
        case type, subject, reason, source, provider, intent, content
        case atRaw = "at"
        case emailAccountID = "email_account_id"
        case emailAccountEmail = "email_account_email"
        case emailAccountName = "email_account_name"
        case campaignID = "campaign_id"
        case campaignName = "campaign_name"
        case stepID = "step_id"
        case stepName = "step_name"
        case taskID = "task_id"
        case scheduledFor = "scheduled_for"
        case joinURL = "join_url"
        case meetingState = "meeting_state"
        case userID = "user_id"
    }

    var at: Date? { ContactsWire.date(atRaw) }
}

/// Timeline envelope: `{ "data": [...], "has_more": bool }` (no pagination key).
struct ContactTimelineResult: Codable, Sendable {
    var data: [ContactTimelineEntry]?
    var hasMore: Bool?

    enum CodingKeys: String, CodingKey {
        case data
        case hasMore = "has_more"
    }
}

// MARK: - Notes (CRM, under contacts)

struct ContactNote: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var contactID: String?
    var organizationID: String?
    var userID: String?
    var content: String?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, content
        case contactID = "contact_id"
        case organizationID = "organization_id"
        case userID = "user_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

// MARK: - Deals (GET /contacts/:id/deals, read-only)

struct ContactDeal: Codable, Identifiable, Sendable {
    var id: String
    var pipelineID: String?
    var stageID: String?
    var contactID: String?
    var name: String?
    var value: Double?
    var currency: String?
    var status: String?
    var expectedCloseDate: Date?
    var wonAt: Date?
    var lostAt: Date?
    var lostReason: String?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, value, currency, status
        case pipelineID = "pipeline_id"
        case stageID = "stage_id"
        case contactID = "contact_id"
        case expectedCloseDate = "expected_close_date"
        case wonAt = "won_at"
        case lostAt = "lost_at"
        case lostReason = "lost_reason"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

/// Read-only pipeline slice, used only to resolve deal stage names/colors.
struct ContactDealStage: Codable, Identifiable, Sendable {
    var id: String
    var name: String?
    var color: String?
    var position: Int?

    enum CodingKeys: String, CodingKey {
        case id, name, color, position
    }
}

struct ContactDealPipeline: Codable, Identifiable, Sendable {
    var id: String
    var name: String?
    var stages: [ContactDealStage]?

    enum CodingKeys: String, CodingKey {
        case id, name, stages
    }
}

// MARK: - Categories (contact tags, from GET /auth/me)

/// Go `models.Group` uses `title` on the wire; the shared `UserGroup` decodes
/// `name`, so categories are re-fetched here with the correct key.
struct ContactCategory: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var title: String?
    var color: String?
    var position: Int?

    enum CodingKeys: String, CodingKey {
        case id, title, color, position
    }
}

struct ContactsMePayload: Codable, Sendable {
    var categories: [ContactCategory]?

    enum CodingKeys: String, CodingKey {
        case categories
    }
}

// MARK: - Request bodies

struct ContactSearchBody: Encodable {
    var query: String? = nil
    var categoryIDs: [String]? = nil
    var subscribed: Bool? = nil
    var minCampaigns: Int? = nil
    var maxCampaigns: Int? = nil
    var campaignIDs: [String]? = nil
    /// Single-campaign Leads view: filter to one derived lead status. Ignored by
    /// the server unless exactly one campaign_id is set.
    var leadStatus: String? = nil
    var customFieldFilters: [ContactFieldFilterPayload]? = nil
    var createdAfter: String? = nil
    var createdBefore: String? = nil
    var updatedAfter: String? = nil
    var updatedBefore: String? = nil
    var sortBy: String? = nil
    var reverse: Bool? = nil

    enum CodingKeys: String, CodingKey {
        case query, subscribed, reverse
        case categoryIDs = "category_ids"
        case minCampaigns = "min_campaigns"
        case maxCampaigns = "max_campaigns"
        case campaignIDs = "campaign_ids"
        case leadStatus = "lead_status"
        case customFieldFilters = "custom_field_filters"
        case createdAfter = "created_after"
        case createdBefore = "created_before"
        case updatedAfter = "updated_after"
        case updatedBefore = "updated_before"
        case sortBy = "sort_by"
    }
}

/// One custom-field criterion on the wire (`{name,value,type}`), matching
/// `models.SearchContactsFilter`. `type` is contains|equal|starts_with|ends_with.
struct ContactFieldFilterPayload: Encodable {
    var name: String
    var value: String
    var type: String
}

// MARK: - Browse facet counts (POST /contacts/search?counts=true)

/// One category's org-wide contact count, from the search `counts` block.
struct ContactCategoryCount: Codable, Sendable, Hashable {
    var categoryID: String
    var count: Int

    enum CodingKeys: String, CodingKey {
        case categoryID = "category_id"
        case count
    }
}

/// Org-wide facet totals for the browse sidebar, returned once (first page)
/// when the search asks for `counts=true`. Independent of the request filters,
/// mirroring the campaigns-overview drawer counts.
struct ContactsCounts: Codable, Sendable {
    var total: Int
    var subscribed: Int
    var unsubscribed: Int
    var inCampaign: Int
    var notContacted: Int
    var categories: [ContactCategoryCount]

    enum CodingKeys: String, CodingKey {
        case total, subscribed, unsubscribed, categories
        case inCampaign = "in_campaign"
        case notContacted = "not_contacted"
    }
}

/// Contacts search page: `data` + `pagination`, plus the optional org-wide
/// `counts` block. (The shared `ListResponse` has no counts field.)
struct ContactSearchPage: Codable, Sendable {
    var data: [Contact]
    var pagination: Pagination?
    var counts: ContactsCounts?
}

/// `models.AddContact`; the endpoint takes an ARRAY of these.
struct ContactCreateBody: Encodable {
    var firstName: String = ""
    var lastName: String = ""
    var email: String
    var company: String = ""
    var phone: String = ""
    var categories: [String]? = nil
    var campaigns: [String]? = nil
    var customFields: [String: String]? = nil

    enum CodingKeys: String, CodingKey {
        case email, company, phone, categories, campaigns
        case firstName = "first_name"
        case lastName = "last_name"
        case customFields = "custom_fields"
    }
}

/// `models.UpdateContact`; nil fields are omitted, which means "leave as-is".
/// `categories`/`campaigns`/`custom_fields` REPLACE the full set when present.
/// Note: email is not updatable server-side.
struct ContactUpdateBody: Encodable {
    var firstName: String? = nil
    var lastName: String? = nil
    var company: String? = nil
    var phone: String? = nil
    var subscribed: Bool? = nil
    var categories: [String]? = nil
    var campaigns: [String]? = nil
    var customFields: [String: String]? = nil

    enum CodingKeys: String, CodingKey {
        case company, phone, subscribed, categories, campaigns
        case firstName = "first_name"
        case lastName = "last_name"
        case customFields = "custom_fields"
    }
}

// MARK: - Advanced search criteria (the filter sheet)

/// A custom-field criterion in the filter sheet (local UI model with a stable
/// id; encoded to `ContactFieldFilterPayload` when the search runs).
struct ContactFieldFilter: Identifiable, Hashable, Sendable {
    var id = UUID()
    var name = ""
    var value = ""
    var type: ContactFilterType = .contains

    var isComplete: Bool {
        !name.trimmingCharacters(in: .whitespaces).isEmpty
            && !value.trimmingCharacters(in: .whitespaces).isEmpty
    }
}

enum ContactFilterType: String, CaseIterable, Hashable, Sendable {
    case contains
    case equal
    case startsWith = "starts_with"
    case endsWith = "ends_with"

    var label: String {
        switch self {
        case .contains: "Contains"
        case .equal: "Equals"
        case .startsWith: "Starts with"
        case .endsWith: "Ends with"
        }
    }
}

enum ContactSort: String, CaseIterable, Hashable, Sendable {
    case createdAt = "created_at"
    case updatedAt = "updated_at"
    case firstName = "first_name"
    case lastName = "last_name"
    case email
    case campaignCount = "campaign_count"

    var label: String {
        switch self {
        case .createdAt: "Date added"
        case .updatedAt: "Last updated"
        case .firstName: "First name"
        case .lastName: "Last name"
        case .email: "Email"
        case .campaignCount: "Campaign count"
        }
    }
}

/// The advanced criteria the filter sheet edits, layered on top of the drawer
/// scope + search text. Empty by default; `isActive` drives the pill badge.
struct ContactAdvancedFilters: Equatable, Sendable {
    var customFields: [ContactFieldFilter] = []
    var categoryIDs: Set<String> = []
    var subscribed: Bool? = nil
    var minCampaigns: Int? = nil
    var maxCampaigns: Int? = nil
    var createdAfter: Date? = nil
    var createdBefore: Date? = nil
    var updatedAfter: Date? = nil
    var updatedBefore: Date? = nil
    var sortBy: ContactSort = .createdAt
    /// Backend `reverse=true` sorts ascending; default is newest-first (DESC).
    var reverse: Bool = false

    /// Number of active criteria (not counting sort), for the pill badge.
    var activeCount: Int {
        var n = customFields.filter(\.isComplete).count
        if !categoryIDs.isEmpty { n += 1 }
        if subscribed != nil { n += 1 }
        if minCampaigns != nil { n += 1 }
        if maxCampaigns != nil { n += 1 }
        if createdAfter != nil { n += 1 }
        if createdBefore != nil { n += 1 }
        if updatedAfter != nil { n += 1 }
        if updatedBefore != nil { n += 1 }
        return n
    }

    var isActive: Bool { activeCount > 0 || sortBy != .createdAt || reverse }
}

// MARK: - Bulk edit (PATCH /contacts, models.BulkEditContactsData)

/// One custom-field op in a bulk edit (`ADD`|`EDIT`|`DELETE`|`RENAME`).
struct ContactBulkFieldPayload: Encodable {
    var type: String
    var key: String
    var value: String
}

/// `models.BulkEditContactsData`: apply category/campaign membership,
/// subscription, and custom-field changes to many contacts in one PATCH.
struct ContactBulkEditBody: Encodable {
    var contacts: [String]
    var addCampaigns: [String]? = nil
    var removeCampaigns: [String]? = nil
    var addCategories: [String]? = nil
    var removeCategories: [String]? = nil
    var fields: [ContactBulkFieldPayload]? = nil
    var subscribe: Bool? = nil

    enum CodingKeys: String, CodingKey {
        case contacts, fields, subscribe
        case addCampaigns = "add_campaigns"
        case removeCampaigns = "remove_campaigns"
        case addCategories = "add_categories"
        case removeCategories = "remove_categories"
    }
}

struct ContactNoteBody: Encodable {
    var content: String

    enum CodingKeys: String, CodingKey {
        case content
    }
}
