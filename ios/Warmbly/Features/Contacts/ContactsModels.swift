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

    enum CodingKeys: String, CodingKey {
        case query, subscribed
        case categoryIDs = "category_ids"
    }
}

/// `models.AddContact`; the endpoint takes an ARRAY of these.
struct ContactCreateBody: Encodable {
    var firstName: String = ""
    var lastName: String = ""
    var email: String
    var company: String = ""
    var phone: String = ""
    var categories: [String]? = nil

    enum CodingKeys: String, CodingKey {
        case email, company, phone, categories
        case firstName = "first_name"
        case lastName = "last_name"
    }
}

/// `models.UpdateContact`; nil fields are omitted, which means "leave as-is".
/// Note: email is not updatable server-side.
struct ContactUpdateBody: Encodable {
    var firstName: String? = nil
    var lastName: String? = nil
    var company: String? = nil
    var phone: String? = nil
    var subscribed: Bool? = nil
    var categories: [String]? = nil

    enum CodingKeys: String, CodingKey {
        case company, phone, subscribed, categories
        case firstName = "first_name"
        case lastName = "last_name"
    }
}

struct ContactNoteBody: Encodable {
    var content: String

    enum CodingKeys: String, CodingKey {
        case content
    }
}
