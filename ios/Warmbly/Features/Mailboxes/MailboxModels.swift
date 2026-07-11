import Foundation
import SwiftUI

// MARK: - Email account (Go models.Email / web Inbox)

/// A connected sender mailbox. List responses omit several columns
/// (warmup_reply_rate, warmup_tag, warmup_pool_type, timezone, user_id,
/// worker_id, organization_id come back as zero values), so everything
/// that isn't structurally guaranteed is optional.
struct EmailAccount: Codable, Identifiable, Hashable, Sendable {
    var id: String
    var userID: String?
    var organizationID: String?
    var workerID: String?
    var email: String
    var name: String?
    var signaturePlain: String?
    var signatureHTML: String?
    var signatureSync: Bool?
    var signatureCode: Bool?
    var provider: String?
    var status: String?
    var lastSyncedAt: Date?
    var lastMessageID: Int64?
    var campaignLimit: Int?
    var minWaitTime: Int?
    var replyTo: String?
    var trackingDomain: String?
    var trackingDomainVerified: Bool?
    var trackingDomainVerifiedAt: Date?
    /// Warmup ramp anchor; non-nil means warmup is enabled.
    var warmup: Date?
    /// Non-nil means warmup is paused (ramp progress kept).
    var warmupPausedAt: Date?
    var warmupBase: Int?
    var warmupMax: Int?
    var warmupIncrease: Int?
    var warmupReplyRate: Int?
    var warmupTag: String?
    var warmupPoolType: String?
    var warmupStartTime: String?
    var warmupEndTime: String?
    var warmupDays: Int?
    var timezone: String?
    var tags: [String]?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, email, name, provider, status, warmup, timezone, tags
        case userID = "user_id"
        case organizationID = "organization_id"
        case workerID = "worker_id"
        case signaturePlain = "signature_plain"
        case signatureHTML = "signature_html"
        case signatureSync = "signature_sync"
        case signatureCode = "signature_code"
        case lastSyncedAt = "last_synced_at"
        case lastMessageID = "last_id"
        case campaignLimit = "campaign_limit"
        case minWaitTime = "min_wait_time"
        case replyTo = "reply_to"
        case trackingDomain = "tracking_domain"
        case trackingDomainVerified = "tracking_domain_verified"
        case trackingDomainVerifiedAt = "tracking_domain_verified_at"
        case warmupPausedAt = "warmup_paused_at"
        case warmupBase = "warmup_base"
        case warmupMax = "warmup_max"
        case warmupIncrease = "warmup_increase"
        case warmupReplyRate = "warmup_reply_rate"
        case warmupTag = "warmup_tag"
        case warmupPoolType = "warmup_pool_type"
        case warmupStartTime = "warmup_start_time"
        case warmupEndTime = "warmup_end_time"
        case warmupDays = "warmup_days"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    /// Derived from the warmup anchor + pause timestamp; never trust a
    /// status string for warmup state.
    var isWarmingActive: Bool { warmup != nil && warmupPausedAt == nil }
    var isWarmupPaused: Bool { warmup != nil && warmupPausedAt != nil }

    var warmupState: MailboxWarmupState {
        if isWarmingActive { return .active }
        if isWarmupPaused { return .paused }
        return .off
    }

    var providerLabel: String {
        switch provider {
        case "gmail": return "Gmail"
        case "outlook": return "Outlook"
        case "smtp_imap": return "SMTP/IMAP"
        default: return provider?.isEmpty == false ? provider! : "Mailbox"
        }
    }

    var statusLabel: String {
        switch status {
        case "active": return "Active"
        case "inactive": return "Inactive"
        case "revoked": return "Revoked"
        default: return status ?? "Unknown"
        }
    }

    var statusTone: Tone {
        switch status {
        case "active": return .emerald
        case "revoked": return .rose
        default: return .slate
        }
    }

    var senderDomain: String {
        email.split(separator: "@").last.map(String.init) ?? email
    }
}

enum MailboxWarmupState: Equatable {
    case active, paused, off

    var title: String {
        switch self {
        case .active: return "Warming"
        case .paused: return "Warmup paused"
        case .off: return "Warmup off"
        }
    }

    var pillText: String {
        switch self {
        case .active: return "warming"
        case .paused: return "paused"
        case .off: return "off"
        }
    }

    var tone: Tone {
        switch self {
        case .active: return .orange
        case .paused: return .amber
        case .off: return .slate
        }
    }
}

// MARK: - Account analytics (GET /analytics/accounts[/:id], Go models.EmailAccountStatus)

struct AccountAnalytics: Codable, Identifiable, Sendable {
    var id: String
    var email: String?
    var provider: String?
    var status: String?
    var lastSyncedAt: Date?
    var health: MailboxHealth?
    var errors: [MailboxAccountError]?
    var dailyUsage: MailboxDailyUsage?
    var warmupStatus: WarmupStatus?
    var warmupHealth: MailboxWarmupHealth?
    var inCampaign: Bool?

    enum CodingKeys: String, CodingKey {
        case id, email, provider, status, health, errors
        case lastSyncedAt = "last_synced_at"
        case dailyUsage = "daily_usage"
        case warmupStatus = "warmup_status"
        case warmupHealth = "warmup_health"
        case inCampaign = "in_campaign"
    }
}

struct MailboxHealth: Codable, Sendable {
    /// "healthy" | "warning" | "error"
    var status: String?
    /// 0-100
    var score: Int?
    var issues: [String]?

    enum CodingKeys: String, CodingKey {
        case status, score, issues
    }

    var tone: Tone {
        switch status {
        case "healthy": return .emerald
        case "warning": return .amber
        case "error": return .rose
        default: return .slate
        }
    }

    var label: String {
        switch status {
        case "healthy": return "Healthy"
        case "warning": return "At risk"
        case "error": return "Issue"
        default: return "Unknown"
        }
    }

    var hasIssue: Bool { status == "warning" || status == "error" }
}

struct MailboxAccountError: Codable, Identifiable, Sendable {
    var id: String
    var errorCode: String?
    var severity: String?
    var title: String?
    var message: String?
    var actionRequired: String?
    var createdAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, severity, title, message
        case errorCode = "error_code"
        case actionRequired = "action_required"
        case createdAt = "created_at"
    }

    var tone: Tone { severity == "CRITICAL" ? .rose : .amber }
}

struct MailboxDailyUsage: Codable, Sendable {
    /// "YYYY-MM-DD"
    var date: String?
    var campaignSent: Int?
    var campaignLimit: Int?
    var warmupSent: Int?
    var warmupLimit: Int?

    enum CodingKeys: String, CodingKey {
        case date
        case campaignSent = "campaign_sent"
        case campaignLimit = "campaign_limit"
        case warmupSent = "warmup_sent"
        case warmupLimit = "warmup_limit"
    }
}

struct WarmupStatus: Codable, Sendable {
    var enabled: Bool?
    var paused: Bool?
    var pausedAt: Date?
    var startedAt: Date?
    var currentVolume: Int?
    var targetVolume: Int?
    var maxVolume: Int?
    var replyRate: Int?
    var daysActive: Int?

    enum CodingKeys: String, CodingKey {
        case enabled, paused
        case pausedAt = "paused_at"
        case startedAt = "started_at"
        case currentVolume = "current_volume"
        case targetVolume = "target_volume"
        case maxVolume = "max_volume"
        case replyRate = "reply_rate"
        case daysActive = "days_active"
    }
}

struct MailboxWarmupHealth: Codable, Sendable {
    /// "healthy" | "watch" | "throttled" | "quarantined" | "blocked"
    var state: String?
    var score: Double?
    var reason: String?
    var spamScore: Int?
    var blockedUntil: Date?
    var evaluatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case state, score, reason
        case spamScore = "spam_score"
        case blockedUntil = "blocked_until"
        case evaluatedAt = "evaluated_at"
    }

    var stateLabel: String {
        switch state {
        case "healthy": return "Healthy"
        case "watch": return "Watch"
        case "throttled": return "Throttled"
        case "quarantined": return "Quarantined"
        case "blocked": return "Blocked"
        default: return state ?? "Unknown"
        }
    }

    var tone: Tone {
        switch state {
        case "healthy": return .emerald
        case "watch", "throttled": return .amber
        case "quarantined", "blocked": return .rose
        default: return .slate
        }
    }

    var isDegraded: Bool { state != nil && state != "healthy" }
}

// MARK: - Domain auth check (GET /emails/:id/auth-check)

struct AuthCheckResult: Codable, Sendable {
    var domain: String?
    var spfFound: Bool?
    var spfRecord: String?
    var dkimFound: Bool?
    var dkimSelectors: [String]?
    var dmarcFound: Bool?
    var dmarcPolicy: String?
    var allAligned: Bool?
    var summary: String?

    enum CodingKeys: String, CodingKey {
        case domain, summary
        case spfFound = "spf_found"
        case spfRecord = "spf_record"
        case dkimFound = "dkim_found"
        case dkimSelectors = "dkim_selectors"
        case dmarcFound = "dmarc_found"
        case dmarcPolicy = "dmarc_policy"
        case allAligned = "all_aligned"
    }
}

// MARK: - Warmup ban status (GET /emails/:id/warmup/ban-status)

struct MailboxBanStatus: Codable, Sendable {
    var emailAccountID: String?
    var blocked: Bool?
    var healthState: String?
    var reason: String?
    var blockedAt: Date?
    var blockedUntil: Date?
    var canAppeal: Bool?
    var pendingAppeal: Bool?

    enum CodingKeys: String, CodingKey {
        case blocked, reason
        case emailAccountID = "email_account_id"
        case healthState = "health_state"
        case blockedAt = "blocked_at"
        case blockedUntil = "blocked_until"
        case canAppeal = "can_appeal"
        case pendingAppeal = "pending_appeal"
    }
}

struct MailboxAppealResponse: Codable, Sendable {
    var appealID: String?

    enum CodingKeys: String, CodingKey {
        case appealID = "appeal_id"
    }
}

// MARK: - Warmup analytics (GET /analytics/warmup)

struct MailboxWarmupAnalytics: Codable, Sendable {
    var emailAccountID: String?
    var email: String?
    var dateRange: MailboxDateRange?
    var summary: MailboxWarmupSummary?
    var dailyStats: [MailboxWarmupDailyStat]?

    enum CodingKeys: String, CodingKey {
        case email, summary
        case emailAccountID = "email_account_id"
        case dateRange = "date_range"
        case dailyStats = "daily_stats"
    }
}

struct MailboxDateRange: Codable, Sendable {
    var from: Date?
    var to: Date?

    enum CodingKeys: String, CodingKey {
        case from, to
    }
}

struct MailboxWarmupSummary: Codable, Sendable {
    var totalSent: Int?
    var totalReplied: Int?
    var averageDaily: Double?
    var replyRate: Double?
    /// Never populated by the backend; always 0. Do not render.
    var targetProgress: Double?
    var daysActive: Int?

    enum CodingKeys: String, CodingKey {
        case totalSent = "total_sent"
        case totalReplied = "total_replied"
        case averageDaily = "average_daily"
        case replyRate = "reply_rate"
        case targetProgress = "target_progress"
        case daysActive = "days_active"
    }
}

struct MailboxWarmupDailyStat: Codable, Sendable {
    /// "YYYY-MM-DD"
    var date: String?
    var emailsSent: Int?
    var emailsReplied: Int?
    var targetVolume: Int?

    enum CodingKeys: String, CodingKey {
        case date
        case emailsSent = "emails_sent"
        case emailsReplied = "emails_replied"
        case targetVolume = "target_volume"
    }

    var day: Date? { MailboxFormat.parseDay(date ?? "") }
}

// MARK: - Connect / onboarding (POST /emails/onboarding/*)

/// `POST /emails/onboarding/oauth/start` body.
struct MailboxOAuthStartBody: Encodable {
    var provider: String
}

/// `POST /emails/onboarding/oauth/start` response.
struct MailboxOAuthStartResponse: Codable, Sendable {
    var url: String
    var state: String
}

/// One SMTP or IMAP endpoint of the SMTP/IMAP connect body.
struct MailboxServerCredentials: Encodable {
    var username: String
    var password: String
    var host: String
    var port: Int
}

/// `POST /emails/onboarding/smtp-imap` body.
struct MailboxSMTPConnectBody: Encodable {
    var email: String
    var name: String
    var smtp: MailboxServerCredentials
    var imap: MailboxServerCredentials
}

// MARK: - Tracking domain (PATCH /emails/:id/track)

struct MailboxTrackingStatus: Codable, Sendable {
    var trackingDomain: String?
    var trackingDomainVerified: Bool?
    var trackingDomainVerifiedAt: Date?

    enum CodingKeys: String, CodingKey {
        case trackingDomain = "tracking_domain"
        case trackingDomainVerified = "tracking_domain_verified"
        case trackingDomainVerifiedAt = "tracking_domain_verified_at"
    }
}

// MARK: - PATCH /emails/:id body (all fields optional; nil = omit)

struct MailboxUpdateBody: Encodable {
    var name: String?
    var signaturePlain: String?
    var signatureHTML: String?
    var signatureSync: Bool?
    var signatureCode: Bool?
    var status: String?
    var campaignLimit: Int?
    var minWaitTime: Int?
    var replyTo: String?
    var warmupBase: Int?
    var warmupMax: Int?
    var warmupIncrease: Int?
    var warmupReplyRate: Int?
    var warmupTag: String?
    var warmupStartTime: String?
    var warmupEndTime: String?
    var warmupDays: Int?
    var tags: [String]?

    enum CodingKeys: String, CodingKey {
        case name, status, tags
        case signaturePlain = "signature_plain"
        case signatureHTML = "signature_html"
        case signatureSync = "signature_sync"
        case signatureCode = "signature_code"
        case campaignLimit = "campaign_limit"
        case minWaitTime = "min_wait_time"
        case replyTo = "reply_to"
        case warmupBase = "warmup_base"
        case warmupMax = "warmup_max"
        case warmupIncrease = "warmup_increase"
        case warmupReplyRate = "warmup_reply_rate"
        case warmupTag = "warmup_tag"
        case warmupStartTime = "warmup_start_time"
        case warmupEndTime = "warmup_end_time"
        case warmupDays = "warmup_days"
    }
}

// MARK: - Formatting helpers

enum MailboxFormat {
    static let day: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter
    }()

    static func parseDay(_ raw: String) -> Date? {
        day.date(from: raw)
    }

    /// Min-gap seconds as terse copy: "600 s" or "10 min".
    static func gap(_ seconds: Int) -> String {
        if seconds >= 60, seconds % 60 == 0 { return "\(seconds / 60) min" }
        return "\(seconds) s"
    }

    /// Weekday bitmask (bit 0 = Sunday, matching Go's time.Weekday in the
    /// scheduler); 0 or 127 = every day. Rendered Mon-first.
    static func weekdays(_ mask: Int) -> String {
        guard mask > 0, mask < 127 else { return "every day" }
        let names = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
        let monFirst = [1, 2, 3, 4, 5, 6, 0]
        let picked = monFirst.filter { mask & (1 << $0) != 0 }.map { names[$0] }
        return picked.joined(separator: " ")
    }
}
