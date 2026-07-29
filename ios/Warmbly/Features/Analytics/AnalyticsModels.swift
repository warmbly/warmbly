import Foundation

// MARK: - Period

/// Dashboard range tabs, mirroring the web's 7d | 30d | 90d toggle.
enum AnalyticsPeriod: String, CaseIterable, Identifiable {
    case week = "7d"
    case month = "30d"
    case quarter = "90d"

    var id: String { rawValue }

    var days: Int {
        switch self {
        case .week: 7
        case .month: 30
        case .quarter: 90
        }
    }
}

// MARK: - Formatting helpers

enum AnalyticsDay {
    /// Chart dates are plain "YYYY-MM-DD" strings (UTC days server-side).
    static let formatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(identifier: "UTC")
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter
    }()

    static func parse(_ raw: String?) -> Date? {
        guard let raw else { return nil }
        return formatter.date(from: raw)
    }

    static func string(from date: Date) -> String {
        formatter.string(from: date)
    }
}

enum AnalyticsFmt {
    /// Server rates are already on the 0-100 percent scale.
    static func rate(_ value: Double?) -> String {
        guard let value else { return "–" }
        if value == 0 { return "0%" }
        if value < 1 { return String(format: "%.2f%%", value) }
        return String(format: "%.1f%%", value)
    }

    static func count(_ value: Int?) -> String {
        WFormat.compact(value ?? 0)
    }
}

/// `{"data": [...]}` envelope where `data` may be JSON null.
struct AnalyticsDataEnvelope<T: Codable & Sendable>: Codable, Sendable {
    var data: [T]?
}

// MARK: - Dashboard (GET analytics/dashboard)

struct DashboardAnalytics: Codable, Sendable {
    var period: String?
    var overallStats: AnalyticsOverallStats?
    var recentActivity: [AnalyticsActivityItem]?
    var topCampaigns: [AnalyticsTopCampaign]?
    var accountHealth: AnalyticsAccountHealthCounts?
    var dailyTrend: [AnalyticsTrendPoint]?

    enum CodingKeys: String, CodingKey {
        case period
        case overallStats = "overall_stats"
        case recentActivity = "recent_activity"
        case topCampaigns = "top_campaigns"
        case accountHealth = "account_health"
        case dailyTrend = "daily_trend"
    }
}

struct AnalyticsOverallStats: Codable, Sendable {
    var totalEmailsSent: Int?
    var totalOpens: Int?
    /// Subset of opens from automated fetchers (Apple MPP etc.).
    var machineOpens: Int?
    var totalClicks: Int?
    var totalReplies: Int?
    var totalBounces: Int?
    var openRate: Double?
    var clickRate: Double?
    var replyRate: Double?
    var bounceRate: Double?
    var activeCampaigns: Int?
    var activeAccounts: Int?

    enum CodingKeys: String, CodingKey {
        case totalEmailsSent = "total_emails_sent"
        case totalOpens = "total_opens"
        case machineOpens = "machine_opens"
        case totalClicks = "total_clicks"
        case totalReplies = "total_replies"
        case totalBounces = "total_bounces"
        case openRate = "open_rate"
        case clickRate = "click_rate"
        case replyRate = "reply_rate"
        case bounceRate = "bounce_rate"
        case activeCampaigns = "active_campaigns"
        case activeAccounts = "active_accounts"
    }
}

struct AnalyticsActivityItem: Codable, Sendable {
    /// "sent" | "opened" | "clicked" | "replied" | "bounced"
    var type: String?
    var campaignID: String?
    var campaignName: String?
    var contactEmail: String?
    var contactID: String?
    var timestamp: Date?
    var link: String?

    enum CodingKeys: String, CodingKey {
        case type, timestamp, link
        case campaignID = "campaign_id"
        case campaignName = "campaign_name"
        case contactEmail = "contact_email"
        case contactID = "contact_id"
    }
}

struct AnalyticsTopCampaign: Codable, Sendable, Identifiable {
    var campaignID: String?
    var name: String?
    var status: String?
    var emailsSent: Int?
    var openRate: Double?
    var clickRate: Double?
    var replyRate: Double?

    var id: String { campaignID ?? name ?? "" }

    enum CodingKeys: String, CodingKey {
        case name, status
        case campaignID = "campaign_id"
        case emailsSent = "emails_sent"
        case openRate = "open_rate"
        case clickRate = "click_rate"
        case replyRate = "reply_rate"
    }
}

struct AnalyticsAccountHealthCounts: Codable, Sendable {
    var totalAccounts: Int?
    var healthyAccounts: Int?
    var warningAccounts: Int?
    var errorAccounts: Int?

    enum CodingKeys: String, CodingKey {
        case totalAccounts = "total_accounts"
        case healthyAccounts = "healthy_accounts"
        case warningAccounts = "warning_accounts"
        case errorAccounts = "error_accounts"
    }
}

struct AnalyticsTrendPoint: Codable, Sendable {
    var date: String?
    var sent: Int?
    var opens: Int?
    var clicks: Int?
    var replies: Int?

    enum CodingKeys: String, CodingKey {
        case date, sent, opens, clicks, replies
    }
}

// MARK: - Deliverability (GET analytics/deliverability)

struct DeliverabilitySummary: Codable, Sendable {
    var from: Date?
    var to: Date?
    var eventsTotal: Int?
    var bounceCount: Int?
    var complaintCount: Int?
    var unsubscribeCount: Int?
    var replyCount: Int?
    var openCount: Int?
    var clickCount: Int?
    var suppressedRecipients: Int?
    var dlqPending: Int?
    var intentPositive: Int?
    var intentNegative: Int?
    var intentOutOfOffice: Int?
    var intentQuestion: Int?
    var intentNeutral: Int?
    var emailsSent: Int?
    var bounceRate: Double?
    var complaintRate: Double?
    var openRate: Double?
    var clickRate: Double?
    var replyRate: Double?
    /// Nil when no seed samples exist in the window.
    var spamPlacementRate: Double?
    var inboxPlacementRate: Double?
    var placementSamples: Int?
    /// "healthy" | "warning" | "quarantine" | "blocked"
    var band: String?
    var timeseries: [AnalyticsDeliverabilityPoint]?
    var byMailbox: [AnalyticsDeliverabilityMailbox]?
    var byCampaign: [AnalyticsDeliverabilityCampaign]?

    enum CodingKeys: String, CodingKey {
        case from, to, band, timeseries
        case eventsTotal = "events_total"
        case bounceCount = "bounce_count"
        case complaintCount = "complaint_count"
        case unsubscribeCount = "unsubscribe_count"
        case replyCount = "reply_count"
        case openCount = "open_count"
        case clickCount = "click_count"
        case suppressedRecipients = "suppressed_recipients"
        case dlqPending = "dlq_pending"
        case intentPositive = "intent_positive"
        case intentNegative = "intent_negative"
        case intentOutOfOffice = "intent_out_of_office"
        case intentQuestion = "intent_question"
        case intentNeutral = "intent_neutral"
        case emailsSent = "emails_sent"
        case bounceRate = "bounce_rate"
        case complaintRate = "complaint_rate"
        case openRate = "open_rate"
        case clickRate = "click_rate"
        case replyRate = "reply_rate"
        case spamPlacementRate = "spam_placement_rate"
        case inboxPlacementRate = "inbox_placement_rate"
        case placementSamples = "placement_samples"
        case byMailbox = "by_mailbox"
        case byCampaign = "by_campaign"
    }
}

struct AnalyticsDeliverabilityPoint: Codable, Sendable {
    var date: String?
    var sent: Int?
    var bounces: Int?
    var complaints: Int?
    var opens: Int?
    var clicks: Int?
    var replies: Int?
    var unsubscribes: Int?

    enum CodingKeys: String, CodingKey {
        case date, sent, bounces, complaints, opens, clicks, replies, unsubscribes
    }
}

struct AnalyticsDeliverabilityMailbox: Codable, Sendable, Identifiable {
    var emailAccountID: String?
    var email: String?
    var sent: Int?
    var bounces: Int?
    var complaints: Int?
    var bounceRate: Double?
    var complaintRate: Double?
    var band: String?

    var id: String { emailAccountID ?? email ?? "" }

    enum CodingKeys: String, CodingKey {
        case email, sent, bounces, complaints, band
        case emailAccountID = "email_account_id"
        case bounceRate = "bounce_rate"
        case complaintRate = "complaint_rate"
    }
}

struct AnalyticsDeliverabilityCampaign: Codable, Sendable, Identifiable {
    var campaignID: String?
    var name: String?
    var sent: Int?
    var bounces: Int?
    var complaints: Int?
    var bounceRate: Double?
    var complaintRate: Double?
    var band: String?

    var id: String { campaignID ?? name ?? "" }

    enum CodingKeys: String, CodingKey {
        case name, sent, bounces, complaints, band
        case campaignID = "campaign_id"
        case bounceRate = "bounce_rate"
        case complaintRate = "complaint_rate"
    }
}

// MARK: - Warmup (GET analytics/warmup)

struct WarmupAnalytics: Codable, Sendable {
    var emailAccountID: String?
    var email: String?
    var dateRange: AnalyticsDateRange?
    var summary: AnalyticsWarmupSummary?
    var dailyStats: [AnalyticsWarmupDay]?

    enum CodingKeys: String, CodingKey {
        case email, summary
        case emailAccountID = "email_account_id"
        case dateRange = "date_range"
        case dailyStats = "daily_stats"
    }
}

struct AnalyticsDateRange: Codable, Sendable {
    var from: Date?
    var to: Date?

    enum CodingKeys: String, CodingKey {
        case from, to
    }
}

struct AnalyticsWarmupSummary: Codable, Sendable {
    var totalSent: Int?
    var totalReplied: Int?
    var averageDaily: Double?
    var replyRate: Double?
    /// Percentage toward the max warmup volume.
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

struct AnalyticsWarmupDay: Codable, Sendable {
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
}

// MARK: - Account health (GET analytics/accounts)

struct AccountHealthRow: Codable, Sendable, Identifiable {
    var id: String
    var email: String?
    var provider: String?
    var status: String?
    var lastSyncedAt: Date?
    var health: AnalyticsAccountHealth?
    var errors: [AnalyticsAccountError]?
    var dailyUsage: AnalyticsAccountUsage?
    var warmupStatus: AnalyticsWarmupStatusInfo?
    var warmupHealth: AnalyticsWarmupHealthInfo?
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

struct AnalyticsAccountHealth: Codable, Sendable {
    /// "healthy" | "warning" | "error"
    var status: String?
    var score: Int?
    var issues: [String]?

    enum CodingKeys: String, CodingKey {
        case status, score, issues
    }
}

struct AnalyticsAccountError: Codable, Sendable, Identifiable {
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
}

struct AnalyticsAccountUsage: Codable, Sendable {
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

struct AnalyticsWarmupStatusInfo: Codable, Sendable {
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

struct AnalyticsWarmupHealthInfo: Codable, Sendable {
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
}

// MARK: - Audit log (GET audit-logs)

struct AuditActor: Codable, Sendable {
    var id: String?
    var firstName: String?
    var lastName: String?
    var email: String?

    enum CodingKeys: String, CodingKey {
        case id, email
        case firstName = "first_name"
        case lastName = "last_name"
    }

    var displayName: String? {
        let name = [firstName, lastName]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
            .joined(separator: " ")
        if !name.isEmpty { return name }
        return email
    }
}

struct AuditLogEntry: Codable, Sendable, Identifiable {
    var id: String
    var orgID: String?
    /// Actor id, kept for backwards compat; prefer `actor`.
    var userID: String?
    /// Nil when the acting user was deleted.
    var actor: AuditActor?
    var actionDate: Date?
    var action: String?
    var entityType: String?
    var entityID: String?
    var ipAddress: String?
    var userAgent: String?
    var changes: [String: String]?
    var metadata: [String: String]?
    var timestamp: Date?

    enum CodingKeys: String, CodingKey {
        case id, actor, action, changes, metadata, timestamp
        case orgID = "org_id"
        case userID = "user_id"
        case actionDate = "action_date"
        case entityType = "entity_type"
        case entityID = "entity_id"
        case ipAddress = "ip_address"
        case userAgent = "user_agent"
    }

    var when: Date? { timestamp ?? actionDate }
}

/// `{"data": [...], "pagination": {"next_cursor", "has_more"}}` where data may be null.
struct AuditLogsPage: Codable, Sendable {
    var data: [AuditLogEntry]?
    var pagination: Pagination?
}
