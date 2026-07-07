import Foundation

// Codable models for the Campaigns module. Field names mirror the Go json
// tags exactly (no snake_case conversion in the decoder).

// MARK: - Campaign

struct Campaign: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var userID: String?
    var name: String
    var description: String?
    var status: String?
    var stopOnReply: Bool?
    var openTracking: Bool?
    var linkTracking: Bool?
    var dailyLimit: Int?
    var timezone: String?
    var startTime: String?
    var endTime: String?
    var startDate: Date?
    var endDate: Date?
    var senderStrategy: String?
    var rotationMode: String?
    var rampEnabled: Bool?
    var lastStatusChangeAt: Date?
    var updatedAt: Date?
    var createdAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, description, status, timezone
        case userID = "user_id"
        case stopOnReply = "stop_on_reply"
        case openTracking = "open_tracking"
        case linkTracking = "link_tracking"
        case dailyLimit = "daily_limit"
        case startTime = "start_time"
        case endTime = "end_time"
        case startDate = "start_date"
        case endDate = "end_date"
        case senderStrategy = "sender_strategy"
        case rotationMode = "rotation_mode"
        case rampEnabled = "ramp_enabled"
        case lastStatusChangeAt = "last_status_change_at"
        case updatedAt = "updated_at"
        case createdAt = "created_at"
    }
}

/// `GET /campaigns` envelope; `data` can be JSON null on empty result sets.
struct CampaignListPage: Codable, Sendable {
    var data: [Campaign]?
    var pagination: Pagination?
}

struct CampaignCreateBody: Encodable {
    var name: String
}

/// `POST /campaigns/:id/start|stop` -> `{"status": "started" | "stopped"}`.
struct CampaignActionResponse: Codable, Sendable {
    var status: String?
}

// MARK: - Status display

/// Display mapping per the design contract: active = emerald "running",
/// completed = emerald "finished", paused* = amber with reason, draft = slate.
struct CampaignStatusInfo {
    let label: String
    let tone: Tone
    let isLive: Bool

    static func from(_ status: String?) -> CampaignStatusInfo {
        switch status {
        case "active":
            CampaignStatusInfo(label: "running", tone: .emerald, isLive: true)
        case "completed":
            CampaignStatusInfo(label: "finished", tone: .emerald, isLive: false)
        case "paused":
            CampaignStatusInfo(label: "paused", tone: .amber, isLive: false)
        case "paused_no_accounts":
            CampaignStatusInfo(label: "no accounts", tone: .amber, isLive: false)
        case "paused_trial_expired":
            CampaignStatusInfo(label: "trial expired", tone: .amber, isLive: false)
        default:
            if status?.hasPrefix("paused") == true {
                CampaignStatusInfo(label: "paused", tone: .amber, isLive: false)
            } else {
                CampaignStatusInfo(label: "draft", tone: .slate, isLive: false)
            }
        }
    }
}

enum CampaignStatusBucket {
    case running, paused, finished, draft
}

extension Campaign {
    var statusInfo: CampaignStatusInfo { .from(status) }

    var statusBucket: CampaignStatusBucket {
        switch status {
        case "active": return .running
        case "completed": return .finished
        default: return status?.hasPrefix("paused") == true ? .paused : .draft
        }
    }

    var canStart: Bool {
        switch status {
        case "draft", "paused", "paused_no_accounts", "paused_trial_expired": return true
        default: return false
        }
    }

    var canPause: Bool { status == "active" }
}

// MARK: - Steps (sequences)

struct CampaignStep: Codable, Identifiable, Sendable {
    var id: String
    var name: String?
    var subject: String?
    var bodyPlain: String?
    var bodyHTML: String?
    var waitAfter: Int?
    var position: Int?
    var kind: String?
    var updatedAt: Date?
    var createdAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, subject, kind
        case bodyPlain = "body_plain"
        case bodyHTML = "body_html"
        case waitAfter = "wait_after"
        case position
        case updatedAt = "updated_at"
        case createdAt = "created_at"
    }

    /// Plaintext preview: body_plain first, else tags stripped from body_html.
    var bodyPreview: String {
        let raw: String
        if let plain = bodyPlain, !plain.isEmpty {
            raw = plain
        } else if let html = bodyHTML, !html.isEmpty {
            raw = html
                .replacingOccurrences(of: "<[^>]+>", with: " ", options: .regularExpression)
                .replacingOccurrences(of: "&nbsp;", with: " ")
                .replacingOccurrences(of: "&amp;", with: "&")
                .replacingOccurrences(of: "&lt;", with: "<")
                .replacingOccurrences(of: "&gt;", with: ">")
        } else {
            return ""
        }
        return raw
            .components(separatedBy: .whitespacesAndNewlines)
            .filter { !$0.isEmpty }
            .joined(separator: " ")
    }
}

// MARK: - Senders

struct CampaignSender: Codable, Identifiable, Sendable {
    var emailAccountID: String
    var weight: Int?
    var lastSentAt: Date?
    var enabled: Bool?

    var id: String { emailAccountID }

    enum CodingKeys: String, CodingKey {
        case emailAccountID = "email_account_id"
        case weight, enabled
        case lastSentAt = "last_sent_at"
    }
}

struct CampaignSendersPage: Codable, Sendable {
    var data: [CampaignSender]?
}

/// Minimal slice of `GET /emails` rows, used only to resolve sender
/// account ids to addresses. The full mailbox model belongs to Accounts.
struct CampaignSenderAccount: Codable, Identifiable, Sendable {
    var id: String
    var email: String?
    var provider: String?
    var status: String?

    enum CodingKeys: String, CodingKey {
        case id, email, provider, status
    }
}

struct CampaignSenderAccountsPage: Codable, Sendable {
    var data: [CampaignSenderAccount]?
}

// MARK: - Analytics

struct CampaignAnalytics: Codable, Sendable {
    var campaignID: String?
    var name: String?
    var status: String?
    var summary: CampaignAnalyticsSummary?
    var steps: [CampaignAnalyticsStep]?
    // date_range deliberately not decoded: the owner-scoped path serializes
    // zero-value timestamps there.

    enum CodingKeys: String, CodingKey {
        case campaignID = "campaign_id"
        case name, status, summary, steps
    }
}

struct CampaignAnalyticsSummary: Codable, Sendable {
    var totalContacts: Int?
    var emailsSent: Int?
    var emailsPending: Int?
    var uniqueOpens: Int?
    var machineOpens: Int?
    var uniqueClicks: Int?
    var replies: Int?
    var bounces: Int?
    var unsubscribes: Int?
    var openRate: Double?
    var clickRate: Double?
    var replyRate: Double?
    var bounceRate: Double?

    enum CodingKeys: String, CodingKey {
        case totalContacts = "total_contacts"
        case emailsSent = "emails_sent"
        case emailsPending = "emails_pending"
        case uniqueOpens = "unique_opens"
        case machineOpens = "machine_opens"
        case uniqueClicks = "unique_clicks"
        case replies, bounces, unsubscribes
        case openRate = "open_rate"
        case clickRate = "click_rate"
        case replyRate = "reply_rate"
        case bounceRate = "bounce_rate"
    }
}

struct CampaignAnalyticsStep: Codable, Identifiable, Sendable {
    var stepID: String?
    var name: String?
    var position: Int?
    var emailsSent: Int?
    var opens: Int?
    var clicks: Int?
    var replies: Int?
    var bounces: Int?

    var id: String { stepID ?? String(position ?? 0) }

    enum CodingKeys: String, CodingKey {
        case stepID = "step_id"
        case name, position, opens, clicks, replies, bounces
        case emailsSent = "emails_sent"
    }
}

struct CampaignDailyStat: Codable, Hashable, Sendable {
    var date: String?
    var sent: Int?
    var opens: Int?
    var clicks: Int?
    var replies: Int?

    enum CodingKeys: String, CodingKey {
        case date, sent, opens, clicks, replies
    }
}

struct CampaignDailyPage: Codable, Sendable {
    var data: [CampaignDailyStat]?
}

/// `GET /analytics/campaigns/compare` result (owner-scoped rows; campaigns
/// created by teammates are silently absent).
struct CampaignComparisonItem: Codable, Sendable {
    var campaignID: String?
    var name: String?
    var status: String?
    var emailsSent: Int?
    var openRate: Double?
    var clickRate: Double?
    var replyRate: Double?
    var bounceRate: Double?

    enum CodingKeys: String, CodingKey {
        case campaignID = "campaign_id"
        case name, status
        case emailsSent = "emails_sent"
        case openRate = "open_rate"
        case clickRate = "click_rate"
        case replyRate = "reply_rate"
        case bounceRate = "bounce_rate"
    }
}

struct CampaignComparisonResult: Codable, Sendable {
    var campaigns: [CampaignComparisonItem]?
}

/// Derived per-row counters shown on the campaigns list.
struct CampaignRowStats: Sendable {
    var sent: Int
    var opened: Int
    var replied: Int
}

// MARK: - Leads (contacts filtered to one campaign)

struct CampaignLead: Codable, Identifiable, Sendable {
    var id: String
    var firstName: String?
    var lastName: String?
    var email: String?
    var company: String?
    var subscribed: Bool?
    var verificationStatus: String?
    var progress: CampaignLeadProgress?
    var createdAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, email, company, subscribed
        case firstName = "first_name"
        case lastName = "last_name"
        case verificationStatus = "verification_status"
        case progress = "campaign_lead"
        case createdAt = "created_at"
    }

    var displayName: String {
        let name = [firstName, lastName]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
            .joined(separator: " ")
        return name.isEmpty ? (email ?? "Unknown") : name
    }

    var hasName: Bool {
        !(firstName ?? "").isEmpty || !(lastName ?? "").isEmpty
    }
}

struct CampaignLeadProgress: Codable, Sendable {
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

    /// Lead status pills: pending = queued slate, active = processing sky,
    /// replied = emerald, bounced = rose, unsubscribed = slate.
    var statusLabel: String {
        switch status {
        case "active": return "processing"
        case "replied": return "replied"
        case "bounced": return "bounced"
        case "unsubscribed": return "unsubscribed"
        default: return "queued"
        }
    }

    var statusTone: Tone {
        switch status {
        case "active": return .sky
        case "replied": return .emerald
        case "bounced": return .rose
        default: return .slate
        }
    }
}

struct CampaignLeadsPage: Codable, Sendable {
    var data: [CampaignLead]?
    var pagination: Pagination?
}

struct CampaignLeadsSearchBody: Encodable {
    var query: String
    var campaignIDs: [String]

    enum CodingKeys: String, CodingKey {
        case query
        case campaignIDs = "campaign_ids"
    }
}

// MARK: - Errors and dates

enum CampaignActionError: LocalizedError {
    case creatorOnly(String)

    var errorDescription: String? {
        switch self {
        case let .creatorOnly(message): return message
        }
    }
}

enum CampaignDates {
    /// `YYYY-MM-DD` for analytics query params and daily-stat parsing.
    static let dayFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter
    }()

    static func param(_ date: Date) -> String {
        dayFormatter.string(from: date)
    }

    static func day(from raw: String?) -> Date? {
        guard let raw else { return nil }
        return dayFormatter.date(from: raw)
    }
}
