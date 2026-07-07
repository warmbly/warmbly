import SwiftUI

// MARK: - Scopes

/// Computed inbox views, derived client-side from `GET /v1/unibox/overview`.
enum UniboxScope: Hashable {
    case all, unread, today, week, awaiting, snoozed, scheduled, uncategorized
    case mailbox(id: String, label: String)
    case category(id: String, label: String, colorHex: String?)

    var title: String {
        switch self {
        case .all: return "All"
        case .unread: return "Unread"
        case .today: return "Today"
        case .week: return "Week"
        case .awaiting: return "Awaiting reply"
        case .snoozed: return "Snoozed"
        case .scheduled: return "Scheduled"
        case .uncategorized: return "Uncategorized"
        case let .mailbox(_, label): return label
        case let .category(_, label, _): return label
        }
    }
}

// MARK: - Search operators

/// Gmail-style operators parsed out of the search field:
/// `from:` `subject:` `label:` `mailbox:` `after:` `before:`
/// `is:unread|snoozed|awaiting|uncategorized`. Values may be quoted
/// (`from:"alice smith"`); anything else is free text (subject search).
struct UniboxParsedQuery {
    var text = ""
    var from: String?
    var subject: String?
    var unread = false
    var snoozed = false
    var awaiting = false
    var uncategorized = false
    var labelNames: [String] = []
    var mailboxTerms: [String] = []
    var after: String?
    var before: String?

    var hasOperators: Bool {
        from != nil || subject != nil || unread || snoozed || awaiting || uncategorized
            || !labelNames.isEmpty || !mailboxTerms.isEmpty || after != nil || before != nil
    }

    var isEmpty: Bool { text.isEmpty && !hasOperators }
}

enum UniboxQueryParser {
    static func parse(_ raw: String) -> UniboxParsedQuery {
        var parsed = UniboxParsedQuery()
        var freeText: [String] = []

        for token in tokenize(raw) {
            guard let colon = token.firstIndex(of: ":"), colon != token.startIndex else {
                freeText.append(token)
                continue
            }
            let key = token[..<colon].lowercased()
            let value = String(token[token.index(after: colon)...])
            switch key {
            case "from", "sender":
                if !value.isEmpty { parsed.from = value }
            case "subject":
                if !value.isEmpty { parsed.subject = value }
            case "label", "in", "category":
                if !value.isEmpty { parsed.labelNames.append(value.lowercased()) }
            case "mailbox", "to", "account":
                if !value.isEmpty { parsed.mailboxTerms.append(value.lowercased()) }
            case "after", "since":
                if !value.isEmpty { parsed.after = value }
            case "before", "until":
                if !value.isEmpty { parsed.before = value }
            case "is":
                switch value.lowercased() {
                case "unread", "unseen": parsed.unread = true
                case "snoozed": parsed.snoozed = true
                case "awaiting": parsed.awaiting = true
                case "uncategorized", "unlabeled", "unlabelled": parsed.uncategorized = true
                default: freeText.append(token)
                }
            case "no", "has":
                if value.lowercased() == "label" || value.lowercased() == "labels" {
                    parsed.uncategorized = (key == "no")
                } else {
                    freeText.append(token)
                }
            default:
                freeText.append(token)
            }
        }
        parsed.text = freeText.joined(separator: " ")
        return parsed
    }

    /// Whitespace split that keeps quoted spans together and strips quotes.
    private static func tokenize(_ raw: String) -> [String] {
        var tokens: [String] = []
        var current = ""
        var inQuotes = false
        for character in raw {
            if character == "\"" {
                inQuotes.toggle()
                continue
            }
            if character.isWhitespace, !inQuotes {
                if !current.isEmpty {
                    tokens.append(current)
                    current = ""
                }
            } else {
                current.append(character)
            }
        }
        if !current.isEmpty { tokens.append(current) }
        return tokens
    }
}

// MARK: - Overview

struct UniboxMailboxCount: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var email: String?
    var name: String?
    var unread: Int?
    var total: Int?

    enum CodingKeys: String, CodingKey {
        case id, email, name, unread, total
    }
}

struct UniboxGroupCount: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var title: String?
    var color: String?
    var unread: Int?
    var total: Int?

    enum CodingKeys: String, CodingKey {
        case id, title, color, unread, total
    }
}

struct UniboxOverview: Codable, Sendable {
    var total: Int?
    var unread: Int?
    var today: Int?
    var week: Int?
    var snoozed: Int?
    var awaitingReply: Int?
    var scheduledPending: Int?
    var scheduledPendingMax: Int?
    var mailboxes: [UniboxMailboxCount]?
    var tags: [UniboxGroupCount]?
    var categories: [UniboxGroupCount]?
    var generatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case total, unread, today, week, snoozed, mailboxes, tags, categories
        case awaitingReply = "awaiting_reply"
        case scheduledPending = "scheduled_pending"
        case scheduledPendingMax = "scheduled_pending_max"
        case generatedAt = "generated_at"
    }
}

// MARK: - Conversation labels

struct UniboxLabel: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var title: String?
    var color: String?

    enum CodingKeys: String, CodingKey {
        case id, title, color
    }
}

// MARK: - Message preview (list + thread rows)

/// `models.EmailMessageStoreDataPreview` — one row per thread in the list,
/// one row per message on the thread endpoint. Bodies are NOT included.
struct UniboxMessage: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var emailID: String?
    var threadID: String?
    var fromAddr: [String]?
    var toAddr: [String]?
    var subject: String?
    var snippet: String?
    var internalDate: Date?
    var seen: Bool?
    var messageCount: Int?
    var hasUnread: Bool?
    var labels: [UniboxLabel]?

    enum CodingKeys: String, CodingKey {
        case id, subject, snippet, seen, labels
        case emailID = "email_id"
        case threadID = "thread_id"
        case fromAddr = "from_addr"
        case toAddr = "to_addr"
        case internalDate = "internal_date"
        case messageCount = "message_count"
        case hasUnread = "has_unread"
    }
}

extension UniboxMessage {
    /// Thread identity: `thread_id`, falling back to the message id for
    /// unthreaded singletons — mirrors `row.thread_id || row.id` on the web.
    var threadKey: String {
        if let threadID, !threadID.isEmpty { return threadID }
        return id
    }

    var sender: String { fromAddr?.first ?? "" }
    var senderDisplay: String { sender.isEmpty ? "Unknown sender" : UniboxAddress.display(sender) }
    var senderBare: String { UniboxAddress.bare(sender) }
    var recipientBare: String { toAddr?.first.map(UniboxAddress.bare) ?? "" }
    var isUnread: Bool { hasUnread ?? (seen == false) }
}

// MARK: - Full message (HTML body)

/// `models.EmailMessage` from `GET /v1/unibox/:id`. Note the literal
/// `"ReplyTo"` json tag (capital R, no underscore) — everything else is snake_case.
struct UniboxMessageDetail: Codable, Sendable {
    var id: String
    var threadID: String?
    var flags: [String]?
    var bcc: [String]?
    var cc: [String]?
    var date: Date?
    var from: [String]?
    var inReplyTo: [String]?
    var messageID: String?
    var replyTo: [String]?
    var to: [String]?
    var subject: String?
    var internalDate: Date?
    var bodyPlain: String?
    var bodyHTML: String?

    enum CodingKeys: String, CodingKey {
        case id, flags, bcc, cc, date, from, to, subject
        case threadID = "thread_id"
        case inReplyTo = "in_reply_to"
        case messageID = "message_id"
        case replyTo = "ReplyTo"
        case internalDate = "internal_date"
        case bodyPlain = "body_plain"
        case bodyHTML = "body_html"
    }
}

// MARK: - Snooze

struct UniboxSnooze: Codable, Identifiable, Sendable {
    var id: String
    var userID: String?
    var threadID: String?
    var snoozedUntil: Date?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id
        case userID = "user_id"
        case threadID = "thread_id"
        case snoozedUntil = "snoozed_until"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

struct UniboxSnoozeRequest: Encodable, Sendable {
    var threadID: String
    var snoozedUntil: Date

    enum CodingKeys: String, CodingKey {
        case threadID = "thread_id"
        case snoozedUntil = "snoozed_until"
    }
}

// MARK: - Scheduled sends

/// `models.UniboxScheduledItem`.
struct ScheduledSend: Codable, Identifiable, Sendable, Hashable {
    var taskID: String
    var scheduledAt: Date?
    var createdAt: Date?
    var accountID: String?
    var accountEmail: String?
    var accountName: String?
    var to: [String]?
    var cc: [String]?
    var bcc: [String]?
    var subject: String?
    var snippet: String?
    var threadID: String?

    var id: String { taskID }

    enum CodingKeys: String, CodingKey {
        case to, cc, bcc, subject, snippet
        case taskID = "task_id"
        case scheduledAt = "scheduled_at"
        case createdAt = "created_at"
        case accountID = "account_id"
        case accountEmail = "account_email"
        case accountName = "account_name"
        case threadID = "thread_id"
    }
}

// MARK: - Seen / reply payloads

/// `models.MarkSeen` — the body IS the echo response shape.
struct UniboxMarkSeenRequest: Codable, Sendable {
    var emailIDs: [String]
    var seen: Bool

    enum CodingKeys: String, CodingKey {
        case seen
        case emailIDs = "email_ids"
    }
}

struct UniboxReplyRequest: Encodable, Sendable {
    var emailAccountID: String
    var to: [String]
    var cc: [String]?
    var bcc: [String]?
    var subject: String
    var bodyHTML: String?
    var bodyPlain: String?
    var inReplyTo: [String]?
    var threadID: String?
    var sendMode: String?
    var scheduledAt: Date?

    enum CodingKeys: String, CodingKey {
        case to, cc, bcc, subject
        case emailAccountID = "email_account_id"
        case bodyHTML = "body_html"
        case bodyPlain = "body_plain"
        case inReplyTo = "in_reply_to"
        case threadID = "thread_id"
        case sendMode = "send_mode"
        case scheduledAt = "scheduled_at"
    }
}

/// `emailsend.SendEmailResponse`.
struct UniboxSendResponse: Codable, Sendable {
    var taskID: String?
    var scheduledAt: Date?
    var sendMode: String?

    enum CodingKeys: String, CodingKey {
        case taskID = "task_id"
        case scheduledAt = "scheduled_at"
        case sendMode = "send_mode"
    }
}

// MARK: - Navigation + compose context

/// Value passed into the thread screen.
struct UniboxThread: Hashable {
    var key: String
    var mailboxID: String?
    var mailboxEmail: String?
    var subject: String?
}

struct UniboxComposeContext {
    var accountID: String
    var accountEmail: String?
    var to: [String]
    var cc: [String]
    var subject: String
    var threadID: String?
    var inReplyTo: [String]
}

enum UniboxSendMode: String, CaseIterable, Identifiable {
    case instant, smart, scheduled

    var id: String { rawValue }

    var label: String {
        switch self {
        case .instant: return "Now"
        case .smart: return "Smart"
        case .scheduled: return "Later"
        }
    }
}

// MARK: - Snooze presets

enum UniboxSnoozePreset: CaseIterable {
    case laterToday, tomorrowMorning, nextWeek

    var label: String {
        switch self {
        case .laterToday: return "In 3 hours"
        case .tomorrowMorning: return "Tomorrow 9 AM"
        case .nextWeek: return "Monday 9 AM"
        }
    }

    var date: Date {
        let calendar = Calendar.current
        switch self {
        case .laterToday:
            return Date().addingTimeInterval(3 * 3600)
        case .tomorrowMorning:
            let tomorrow = calendar.date(byAdding: .day, value: 1, to: calendar.startOfDay(for: .now)) ?? .now
            return calendar.date(bySettingHour: 9, minute: 0, second: 0, of: tomorrow)
                ?? Date().addingTimeInterval(24 * 3600)
        case .nextWeek:
            let components = DateComponents(hour: 9, minute: 0, weekday: 2)
            return calendar.nextDate(after: .now, matching: components, matchingPolicy: .nextTime)
                ?? Date().addingTimeInterval(7 * 24 * 3600)
        }
    }
}

// MARK: - Address + formatting helpers

enum UniboxAddress {
    /// Extracts the bare address from `"Display Name <addr>"` forms.
    static func bare(_ raw: String) -> String {
        if let start = raw.firstIndex(of: "<"), let end = raw.firstIndex(of: ">"), start < end {
            return String(raw[raw.index(after: start) ..< end]).trimmingCharacters(in: .whitespaces)
        }
        return raw.trimmingCharacters(in: .whitespaces)
    }

    static func display(_ raw: String) -> String {
        if let start = raw.firstIndex(of: "<") {
            let name = String(raw[..<start]).trimmingCharacters(in: CharacterSet(charactersIn: " \t\"'"))
            if !name.isEmpty { return name }
        }
        return bare(raw)
    }
}

enum UniboxFormat {
    private static let timeOnly: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "h:mm a"
        return formatter
    }()

    private static let weekday: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "EEE"
        return formatter
    }()

    private static let shortDate: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "MMM d"
        return formatter
    }()

    private static let absolute: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter
    }()

    private static let dayParamFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter
    }()

    /// Compact inbox timestamp: time today, weekday this week, else "Jul 1".
    static func listTime(_ date: Date?) -> String {
        guard let date else { return "" }
        let calendar = Calendar.current
        if calendar.isDateInToday(date) { return timeOnly.string(from: date) }
        let days = calendar.dateComponents(
            [.day],
            from: calendar.startOfDay(for: date),
            to: calendar.startOfDay(for: .now)
        ).day ?? 99
        if days >= 0, days < 7 { return weekday.string(from: date) }
        return shortDate.string(from: date)
    }

    static func absoluteTime(_ date: Date?) -> String {
        guard let date else { return "" }
        return absolute.string(from: date)
    }

    /// `since`/`until` param: local `yyyy-MM-dd`.
    static func dayParam(daysBack: Int) -> String {
        let calendar = Calendar.current
        let day = calendar.date(byAdding: .day, value: -daysBack, to: calendar.startOfDay(for: .now)) ?? .now
        return dayParamFormatter.string(from: day)
    }
}

extension Color {
    /// Parses server label colors like "#22c55e".
    init?(uniboxHex raw: String?) {
        guard let raw else { return nil }
        var cleaned = raw.trimmingCharacters(in: .whitespaces)
        if cleaned.hasPrefix("#") { cleaned.removeFirst() }
        guard cleaned.count == 6, let value = UInt32(cleaned, radix: 16) else { return nil }
        self.init(hex: value)
    }
}

// MARK: - AI writing assistant

/// `POST /v1/generation/write` result; 402 means the org is out of credits.
struct AIWriteResponse: Codable, Sendable {
    var text: String
    var creditsRemaining: Int?
    var model: String?

    enum CodingKeys: String, CodingKey {
        case text, model
        case creditsRemaining = "credits_remaining"
    }
}

/// Tone presets mirrored from the web "Write with AI" popover.
enum AIWriteTone: String, CaseIterable, Identifiable {
    case standard = ""
    case friendly, professional, casual, concise, persuasive

    var id: String { rawValue }

    var label: String {
        self == .standard ? "Default" : rawValue.capitalized
    }
}

// MARK: - Thread label editing

/// `PUT /v1/unibox/thread/labels` body: replaces the full label set.
struct UniboxSetLabelsRequest: Encodable, Sendable {
    var threadID: String
    var categoryIDs: [String]

    enum CodingKeys: String, CodingKey {
        case threadID = "thread_id"
        case categoryIDs = "category_ids"
    }
}

/// `GET /v1/contacts/lookup` wraps the match in a `contact` key.
struct ContactLookupEnvelope: Codable, Sendable {
    var contact: Contact?
}
