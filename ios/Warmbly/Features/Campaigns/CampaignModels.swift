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
    var textOnly: Bool?
    var unsubscribeHeader: Bool?
    var riskyEmails: Bool?
    var dailyLimit: Int?
    var cc: [String]?
    var bcc: [String]?
    var timezone: String?
    /// Legacy day bitmask, Monday-indexed (bit 0 = Monday).
    var days: Int?
    var startTime: String?
    var endTime: String?
    var startDate: Date?
    var endDate: Date?
    /// Authoritative per-day windows when any day is non-empty; wire-indexed
    /// by time.Weekday (0=Sunday). Empty days arrive as JSON null.
    var scheduleWindows: [[ScheduleInterval]?]?
    var emailTags: [String]?
    var folders: [String]?
    var contactOrderBy: String?
    var contactOrderDir: String?
    var senderStrategy: String?
    var rotationMode: String?
    var rampEnabled: Bool?
    var rampStart: Int?
    var rampIncrement: Int?
    var rampCeiling: Int?
    var rampLevel: Int?
    var espMatchMode: String?
    var maxNewLeadsPerDay: Int?
    var prioritizeNewLeads: Bool?
    var trackingDomain: String?
    var trackingDomainVerified: Bool?
    var lastStatusChangeAt: Date?
    var updatedAt: Date?
    var createdAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, description, status, timezone, days, cc, bcc, folders
        case userID = "user_id"
        case stopOnReply = "stop_on_reply"
        case openTracking = "open_tracking"
        case linkTracking = "link_tracking"
        case textOnly = "text_only"
        case unsubscribeHeader = "unsubscribe_header"
        case riskyEmails = "risky_emails"
        case dailyLimit = "daily_limit"
        case startTime = "start_time"
        case endTime = "end_time"
        case startDate = "start_date"
        case endDate = "end_date"
        case scheduleWindows = "schedule_windows"
        case emailTags = "email_tags"
        case contactOrderBy = "contact_order_by"
        case contactOrderDir = "contact_order_dir"
        case senderStrategy = "sender_strategy"
        case rotationMode = "rotation_mode"
        case rampEnabled = "ramp_enabled"
        case rampStart = "ramp_start"
        case rampIncrement = "ramp_increment"
        case rampCeiling = "ramp_ceiling"
        case rampLevel = "ramp_level"
        case espMatchMode = "esp_match_mode"
        case maxNewLeadsPerDay = "max_new_leads_per_day"
        case prioritizeNewLeads = "prioritize_new_leads"
        case trackingDomain = "tracking_domain"
        case trackingDomainVerified = "tracking_domain_verified"
        case lastStatusChangeAt = "last_status_change_at"
        case updatedAt = "updated_at"
        case createdAt = "created_at"
    }
}

/// One sending window in minutes since local midnight.
struct ScheduleInterval: Codable, Hashable, Sendable {
    var start: Int
    var end: Int
}

/// `GET /campaigns` envelope; `data` can be JSON null on empty result sets.
struct CampaignListPage: Codable, Sendable {
    var data: [Campaign]?
    var pagination: Pagination?
}

/// `GET /campaigns-overview` — status-bucket and per-folder counts that
/// drive the campaigns browser sidebar.
struct CampaignsOverview: Codable, Sendable {
    var total: Int?
    var active: Int?
    var paused: Int?
    var draft: Int?
    var completed: Int?
    var folders: [CampaignFolderCount]?
}

struct CampaignFolderCount: Codable, Sendable {
    var folderID: String
    var total: Int?

    enum CodingKeys: String, CodingKey {
        case folderID = "folder_id"
        case total
    }
}

struct CampaignCreateBody: Encodable {
    var name: String
    var description: String?
    var dailyLimit: Int?
    var stopOnReply: Bool?
    var openTracking: Bool?
    var linkTracking: Bool?
    var folderIDs: [String]?

    enum CodingKeys: String, CodingKey {
        case name, description
        case dailyLimit = "daily_limit"
        case stopOnReply = "stop_on_reply"
        case openTracking = "open_tracking"
        case linkTracking = "link_tracking"
        case folderIDs = "folder_ids"
    }
}

/// Partial `PATCH /campaigns/:id` body. Synthesized Encodable omits nil
/// fields, so each call sends only what it means to change.
struct CampaignUpdateBody: Encodable {
    var name: String? = nil
    var description: String? = nil
    var dailyLimit: Int? = nil
    var stopOnReply: Bool? = nil
    var openTracking: Bool? = nil
    var linkTracking: Bool? = nil
    var textOnly: Bool? = nil
    var unsubscribeHeader: Bool? = nil
    var riskyEmails: Bool? = nil
    var timezone: String? = nil
    var scheduleWindows: [[ScheduleInterval]]? = nil
    var senderStrategy: String? = nil
    var rotationMode: String? = nil
    var espMatchMode: String? = nil
    var maxNewLeadsPerDay: Int? = nil
    var prioritizeNewLeads: Bool? = nil
    var rampEnabled: Bool? = nil
    var rampStart: Int? = nil
    var rampIncrement: Int? = nil
    var rampCeiling: Int? = nil

    enum CodingKeys: String, CodingKey {
        case name, description, timezone
        case dailyLimit = "daily_limit"
        case stopOnReply = "stop_on_reply"
        case openTracking = "open_tracking"
        case linkTracking = "link_tracking"
        case textOnly = "text_only"
        case unsubscribeHeader = "unsubscribe_header"
        case riskyEmails = "risky_emails"
        case scheduleWindows = "schedule_windows"
        case senderStrategy = "sender_strategy"
        case rotationMode = "rotation_mode"
        case espMatchMode = "esp_match_mode"
        case maxNewLeadsPerDay = "max_new_leads_per_day"
        case prioritizeNewLeads = "prioritize_new_leads"
        case rampEnabled = "ramp_enabled"
        case rampStart = "ramp_start"
        case rampIncrement = "ramp_increment"
        case rampCeiling = "ramp_ceiling"
    }
}

/// `POST /campaigns/:id/start|stop` -> `{"status": "started" | "stopped"}`.
struct CampaignActionResponse: Codable, Sendable {
    var status: String?
}

// MARK: - Schedule

/// A schedule expressible as "these days, one shared window": what the mobile
/// editor can safely round-trip. Multi-window or per-day-differing schedules
/// stay read-only on mobile (edited on the web).
struct CampaignSimpleSchedule: Equatable {
    /// Monday-first day indices (0 = Monday .. 6 = Sunday).
    var days: Set<Int>
    var startMinutes: Int
    var endMinutes: Int

    /// Wire shape for `schedule_windows`: 7 arrays indexed 0=Sun..6=Sat.
    var wireWindows: [[ScheduleInterval]] {
        var wire: [[ScheduleInterval]] = Array(repeating: [], count: 7)
        for day in days {
            wire[(day + 1) % 7] = [ScheduleInterval(start: startMinutes, end: endMinutes)]
        }
        return wire
    }

    static let dayLetters = ["M", "T", "W", "T", "F", "S", "S"]
    static let dayNames = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"]

    static func minutesLabel(_ minutes: Int) -> String {
        String(format: "%02d:%02d", minutes / 60, minutes % 60)
    }

    var daysLabel: String {
        if days.count == 7 { return "Every day" }
        if days == Set(0...4) { return "Weekdays" }
        if days == Set([5, 6]) { return "Weekends" }
        if days.count > 3 { return "\(days.count) days" }
        return days.sorted().map { Self.dayNames[$0].prefix(3) }.joined(separator: " ")
    }

    var summary: String {
        "\(daysLabel) · \(Self.minutesLabel(startMinutes))–\(Self.minutesLabel(endMinutes))"
    }
}

extension Campaign {
    /// Per-day windows with wire nulls normalized to empty arrays.
    var windows: [[ScheduleInterval]] {
        let raw = (scheduleWindows ?? []).map { $0 ?? [] }
        return raw.count == 7 ? raw : Array(repeating: [], count: 7)
    }

    /// Seed the drag board in DISPLAY order (Monday = 0). Prefers the wire
    /// windows (Sun-indexed); otherwise derives one window per active legacy
    /// day. Matches the web editor's `seedWindows`.
    func seedDisplayWindows() -> [[ScheduleInterval]] {
        let wire = windows
        if wire.contains(where: { !$0.isEmpty }) {
            return (0..<7).map { wire[($0 + 1) % 7] }
        }
        guard let start = Self.minutes(from: startTime),
              let end = Self.minutes(from: endTime), end > start else {
            return Array(repeating: [], count: 7)
        }
        let mask = days ?? 0
        return (0..<7).map { mask & (1 << $0) != 0 ? [ScheduleInterval(start: start, end: end)] : [] }
    }

    /// DISPLAY order (Mon=0) back to the wire `schedule_windows` shape (Sun=0).
    static func wireWindows(fromDisplay display: [[ScheduleInterval]]) -> [[ScheduleInterval]] {
        var wire = Array(repeating: [ScheduleInterval](), count: 7)
        for i in 0..<7 where i < display.count { wire[(i + 1) % 7] = display[i] }
        return wire
    }

    var hasCustomWindows: Bool {
        windows.contains { !$0.isEmpty } && simpleSchedule == nil
    }

    private static func minutes(from raw: String?) -> Int? {
        guard let raw, raw.count >= 5 else { return nil }
        let parts = raw.prefix(5).split(separator: ":")
        guard parts.count == 2, let h = Int(parts[0]), let m = Int(parts[1]) else { return nil }
        return h * 60 + m
    }

    /// nil when the schedule can't be expressed as one shared daily window.
    var simpleSchedule: CampaignSimpleSchedule? {
        let wire = windows
        if wire.contains(where: { !$0.isEmpty }) {
            var days = Set<Int>()
            var shared: ScheduleInterval?
            for (weekday, intervals) in wire.enumerated() where !intervals.isEmpty {
                guard intervals.count == 1 else { return nil }
                if let shared, shared != intervals[0] { return nil }
                shared = intervals[0]
                days.insert((weekday + 6) % 7)
            }
            guard let shared else { return nil }
            return CampaignSimpleSchedule(days: days, startMinutes: shared.start, endMinutes: shared.end)
        }
        // Legacy fields: Monday-indexed bitmask + one window.
        guard let start = Self.minutes(from: startTime),
              let end = Self.minutes(from: endTime), end > start else { return nil }
        let mask = days ?? 0
        let active = Set((0..<7).filter { mask & (1 << $0) != 0 })
        guard !active.isEmpty else { return nil }
        return CampaignSimpleSchedule(days: active, startMinutes: start, endMinutes: end)
    }

    var scheduleSummary: String {
        if let simple = simpleSchedule { return simple.summary }
        let activeDays = windows.filter { !$0.isEmpty }.count
        if activeDays > 0 {
            return "Custom · \(activeDays) day\(activeDays == 1 ? "" : "s")"
        }
        return "Not set"
    }
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

extension CampaignLead: Hashable {
    // Identity by id: enough for navigation and list diffing; the row re-renders
    // from the value when the list reloads.
    static func == (lhs: CampaignLead, rhs: CampaignLead) -> Bool { lhs.id == rhs.id }
    func hash(into hasher: inout Hasher) { hasher.combine(id) }
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
    /// Per-status lead totals for the scope chips (first page, single campaign).
    var leadCounts: CampaignLeadCounts?

    enum CodingKeys: String, CodingKey {
        case data, pagination
        case leadCounts = "lead_counts"
    }
}

/// Per-status lead totals within one campaign (`lead_counts` block).
struct CampaignLeadCounts: Codable, Sendable {
    var total: Int?
    var queued: Int?
    var processing: Int?
    var replied: Int?
    var bounced: Int?
    var unsubscribed: Int?
}

/// The Leads browser's status scopes, mirroring the contacts browse scopes but
/// scoped inside one campaign. Each maps to the server's `lead_status` filter
/// (nil for "All"), and reads its live total from `CampaignLeadCounts`.
enum CampaignLeadScope: String, CaseIterable, Identifiable, Hashable {
    case all, processing, replied, bounced, queued, unsubscribed

    var id: String { rawValue }

    var title: String {
        switch self {
        case .all: "All"
        case .processing: "Processing"
        case .replied: "Replied"
        case .bounced: "Bounced"
        case .queued: "Queued"
        case .unsubscribed: "Unsubscribed"
        }
    }

    /// The wire `lead_status` value; nil means no status filter (All).
    var leadStatus: String? {
        switch self {
        case .all: nil
        case .processing: "active"
        case .replied: "replied"
        case .bounced: "bounced"
        case .queued: "pending"
        case .unsubscribed: "unsubscribed"
        }
    }

    var icon: String {
        switch self {
        case .all: "person.2.fill"
        case .processing: "paperplane.fill"
        case .replied: "arrowshape.turn.up.left.fill"
        case .bounced: "exclamationmark.arrow.circlepath"
        case .queued: "hourglass"
        case .unsubscribed: "bell.slash.fill"
        }
    }

    var tone: Tone {
        switch self {
        case .all: .sky
        case .processing: .sky
        case .replied: .emerald
        case .bounced: .rose
        case .queued: .slate
        case .unsubscribed: .slate
        }
    }

    func count(_ counts: CampaignLeadCounts?) -> Int {
        guard let counts else { return 0 }
        switch self {
        case .all: return counts.total ?? 0
        case .processing: return counts.processing ?? 0
        case .replied: return counts.replied ?? 0
        case .bounced: return counts.bounced ?? 0
        case .queued: return counts.queued ?? 0
        case .unsubscribed: return counts.unsubscribed ?? 0
        }
    }
}

extension CampaignLeadProgress {
    /// The engagement funnel shown in the lead detail, in order.
    var funnel: [(label: String, value: Int, tone: Tone)] {
        [
            ("Sent", sent ?? 0, .sky),
            ("Opened", opened ?? 0, .indigo),
            ("Clicked", clicked ?? 0, .amber),
            ("Replied", replied ?? 0, .emerald),
        ]
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
