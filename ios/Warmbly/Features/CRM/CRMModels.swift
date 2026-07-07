import Foundation

// Wire models for CRM (pipelines, deals, tasks, meetings) and reply templates.
// JSON field names copied from the Go json tags; explicit CodingKeys everywhere.

// MARK: - Envelopes

/// List envelope tolerant of `data: null` on empty result sets.
struct CRMPage<T: Decodable & Sendable>: Decodable, Sendable {
    var data: [T]?
    var pagination: Pagination?

    var items: [T] { data ?? [] }
}

/// `DELETE /meetings/:id` returns `200 {"deleted": true}` (not 204).
struct CRMDeletedResponse: Decodable, Sendable {
    var deleted: Bool?
}

// MARK: - Pipelines

struct CRMPipeline: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var organizationID: String?
    var name: String
    var position: Int?
    var stages: [CRMStage]?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, position, stages
        case organizationID = "organization_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    var orderedStages: [CRMStage] {
        (stages ?? []).sorted { ($0.position ?? 0) < ($1.position ?? 0) }
    }
}

struct CRMStage: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var pipelineID: String?
    var name: String
    var color: String?
    var position: Int?
    /// Open deals in this stage (omitempty on the wire).
    var dealCount: Int?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, color, position
        case pipelineID = "pipeline_id"
        case dealCount = "deal_count"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

// MARK: - Deals

/// Minimal joined contact on deal rows (full Contact type belongs to the
/// Contacts module; only decode what the deal list renders).
struct CRMDealContact: Codable, Sendable, Hashable {
    var id: String?
    var firstName: String?
    var lastName: String?
    var email: String?
    var company: String?

    enum CodingKeys: String, CodingKey {
        case id, email, company
        case firstName = "first_name"
        case lastName = "last_name"
    }

    var displayName: String {
        let name = [firstName, lastName]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
            .joined(separator: " ")
        if !name.isEmpty { return name }
        return email ?? "No contact"
    }
}

struct CRMDeal: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var organizationID: String?
    var pipelineID: String?
    var stageID: String?
    var contactID: String?
    var name: String
    var value: Double?
    var currency: String?
    /// "open" | "won" | "lost"
    var status: String?
    var expectedCloseDate: Date?
    var wonAt: Date?
    var lostAt: Date?
    var lostReason: String?
    var assignedTo: String?
    var campaignID: String?
    var sourceMailboxID: String?
    var createdAt: Date?
    var updatedAt: Date?
    // Joined fields, list/search responses only.
    var contact: CRMDealContact?
    var stage: CRMStage?
    var campaignName: String?

    enum CodingKeys: String, CodingKey {
        case id, name, value, currency, status, contact, stage
        case organizationID = "organization_id"
        case pipelineID = "pipeline_id"
        case stageID = "stage_id"
        case contactID = "contact_id"
        case expectedCloseDate = "expected_close_date"
        case wonAt = "won_at"
        case lostAt = "lost_at"
        case lostReason = "lost_reason"
        case assignedTo = "assigned_to"
        case campaignID = "campaign_id"
        case sourceMailboxID = "source_mailbox_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case campaignName = "campaign_name"
    }
}

struct CRMDealsSummaryStage: Codable, Sendable {
    var stageID: String?
    var count: Int?
    var value: Double?

    enum CodingKeys: String, CodingKey {
        case count, value
        case stageID = "stage_id"
    }
}

struct CRMDealsSummary: Codable, Sendable {
    var total: Int?
    var openCount: Int?
    var openValue: Double?
    var wonCount: Int?
    var wonValue: Double?
    var lostCount: Int?
    var lostValue: Double?
    var currency: String?
    var mixedCurrency: Bool?
    var stages: [CRMDealsSummaryStage]?

    enum CodingKeys: String, CodingKey {
        case total, currency, stages
        case openCount = "open_count"
        case openValue = "open_value"
        case wonCount = "won_count"
        case wonValue = "won_value"
        case lostCount = "lost_count"
        case lostValue = "lost_value"
        case mixedCurrency = "mixed_currency"
    }
}

/// `POST /crm/deals/search` + `/crm/deals/summary` body (all fields optional;
/// an all-nil struct encodes as `{}`, which the backend requires at minimum).
struct CRMDealSearchBody: Encodable, Sendable {
    var query: String?
    var statuses: [String]?
    var pipelineIDs: [String]?
    var stageIDs: [String]?
    var sortBy: String?
    var reverse: Bool?

    enum CodingKeys: String, CodingKey {
        case query, statuses, reverse
        case pipelineIDs = "pipeline_ids"
        case stageIDs = "stage_ids"
        case sortBy = "sort_by"
    }
}

/// `PATCH /crm/deals/:id` body; send only the fields being changed.
struct CRMDealUpdateBody: Encodable, Sendable {
    var stageID: String? = nil
    var name: String? = nil
    var value: Double? = nil
    var currency: String? = nil
    var status: String? = nil
    var expectedCloseDate: Date? = nil
    var lostReason: String? = nil

    enum CodingKeys: String, CodingKey {
        case name, value, currency, status
        case stageID = "stage_id"
        case expectedCloseDate = "expected_close_date"
        case lostReason = "lost_reason"
    }
}

// MARK: - Tasks

struct CRMTask: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var organizationID: String?
    var contactID: String?
    var dealID: String?
    var assignedTo: String?
    var assignedTeamID: String?
    var createdBy: String?
    var title: String
    var description: String?
    var dueDate: Date?
    /// "low" | "medium" | "high" | "urgent"
    var priority: String?
    /// Task-type NAME string, "" = none.
    var type: String?
    /// "pending" | "in_progress" | "completed" | "cancelled"
    var status: String?
    var completedAt: Date?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, title, description, priority, type, status
        case organizationID = "organization_id"
        case contactID = "contact_id"
        case dealID = "deal_id"
        case assignedTo = "assigned_to"
        case assignedTeamID = "assigned_team_id"
        case createdBy = "created_by"
        case dueDate = "due_date"
        case completedAt = "completed_at"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    var isDone: Bool { status == "completed" || status == "cancelled" }

    var isOverdue: Bool {
        guard let dueDate, !isDone else { return false }
        return dueDate < Date()
    }
}

struct CRMTaskType: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var organizationID: String?
    var name: String
    var color: String?
    var position: Int?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, color, position
        case organizationID = "organization_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

struct CRMTasksSummary: Codable, Sendable {
    var total: Int?
    var pendingCount: Int?
    var inProgressCount: Int?
    var completedCount: Int?
    var cancelledCount: Int?
    var overdueCount: Int?
    var highPriorityCount: Int?

    enum CodingKeys: String, CodingKey {
        case total
        case pendingCount = "pending_count"
        case inProgressCount = "in_progress_count"
        case completedCount = "completed_count"
        case cancelledCount = "cancelled_count"
        case overdueCount = "overdue_count"
        case highPriorityCount = "high_priority_count"
    }

    var openCount: Int { (pendingCount ?? 0) + (inProgressCount ?? 0) }
}

/// `POST /crm/tasks/search` + `/crm/tasks/summary` body.
struct CRMTaskSearchBody: Encodable, Sendable {
    var query: String?
    var statuses: [String]?
    var overdue: Bool?
    var sortBy: String?
    var reverse: Bool?

    enum CodingKeys: String, CodingKey {
        case query, statuses, overdue, reverse
        case sortBy = "sort_by"
    }
}

/// `POST /crm/tasks` body.
struct CRMTaskCreateBody: Encodable, Sendable {
    var title: String
    var description: String? = nil
    var dueDate: Date? = nil
    var priority: String? = nil
    var type: String? = nil

    enum CodingKeys: String, CodingKey {
        case title, description, priority, type
        case dueDate = "due_date"
    }
}

/// `PATCH /crm/tasks/:id` body (needs at least one field).
struct CRMTaskUpdateBody: Encodable, Sendable {
    var title: String? = nil
    var description: String? = nil
    var dueDate: Date? = nil
    var priority: String? = nil
    var type: String? = nil
    var status: String? = nil

    enum CodingKeys: String, CodingKey {
        case title, description, priority, type, status
        case dueDate = "due_date"
    }
}

// MARK: - Meetings

struct CRMMeeting: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var organizationID: String?
    /// "manual" | "calendly" | "cal_com"
    var source: String?
    var externalEventID: String?
    /// "booked" | "rescheduled" | "canceled" | "completed" | "no_show"
    var status: String?
    var inviteeEmail: String?
    var inviteeName: String?
    var eventName: String?
    var eventType: String?
    var scheduledFor: Date?
    var endTime: Date?
    var joinURL: String?
    var location: String?
    var cancelURL: String?
    var rescheduleURL: String?
    var canceledReason: String?
    var contactID: String?
    var campaignID: String?
    var createdAt: Date?
    var updatedAt: Date?
    var contactName: String?

    enum CodingKeys: String, CodingKey {
        case id, source, status, location
        case organizationID = "organization_id"
        case externalEventID = "external_event_id"
        case inviteeEmail = "invitee_email"
        case inviteeName = "invitee_name"
        case eventName = "event_name"
        case eventType = "event_type"
        case scheduledFor = "scheduled_for"
        case endTime = "end_time"
        case joinURL = "join_url"
        case cancelURL = "cancel_url"
        case rescheduleURL = "reschedule_url"
        case canceledReason = "canceled_reason"
        case contactID = "contact_id"
        case campaignID = "campaign_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case contactName = "contact_name"
    }

    var displayTitle: String {
        if let eventName, !eventName.isEmpty { return eventName }
        if let inviteeName, !inviteeName.isEmpty { return inviteeName }
        return inviteeEmail ?? "Meeting"
    }

    var inviteeLabel: String? {
        if let inviteeName, !inviteeName.isEmpty { return inviteeName }
        if let inviteeEmail, !inviteeEmail.isEmpty { return inviteeEmail }
        return nil
    }

    var sourceLabel: String {
        switch source {
        case "calendly": return "Calendly"
        case "cal_com": return "Cal.com"
        case "manual": return "Manual"
        default: return source ?? ""
        }
    }

    var isToday: Bool {
        guard let scheduledFor else { return false }
        return Calendar.current.isDateInToday(scheduledFor)
    }
}

/// `GET /meetings/summary`.
struct CRMMeetingsSummary: Codable, Sendable {
    var upcoming: Int?
    var today: Int?
    var total: Int?
    var canceled: Int?

    enum CodingKeys: String, CodingKey {
        case upcoming, today, total, canceled
    }
}

// MARK: - Templates

/// Reply template (`/v1/templates`, Go `models.ReplyTemplate`).
struct EmailTemplate: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var organizationID: String?
    var userID: String?
    var name: String
    var subject: String?
    var bodyHTML: String?
    var bodyPlain: String?
    var position: Int?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, subject, position
        case organizationID = "organization_id"
        case userID = "user_id"
        case bodyHTML = "body_html"
        case bodyPlain = "body_plain"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    var snippet: String {
        if let plain = bodyPlain?.trimmingCharacters(in: .whitespacesAndNewlines), !plain.isEmpty {
            return plain.replacingOccurrences(of: "\n", with: " ")
        }
        if let subject, !subject.isEmpty { return subject }
        return "No content"
    }
}

/// `POST /templates/:id/render` body.
struct TemplateRenderBody: Encodable, Sendable {
    var variables: [String: String]
}

/// `POST /templates/:id/render` response.
struct TemplateRenderResult: Decodable, Sendable {
    var subject: String?
    var bodyHTML: String?
    var bodyPlain: String?

    enum CodingKeys: String, CodingKey {
        case subject
        case bodyHTML = "body_html"
        case bodyPlain = "body_plain"
    }
}
