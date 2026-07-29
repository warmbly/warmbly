import Foundation

// MARK: - Mailbox candidates (From picker)

/// One scored sender from `GET /v1/unibox/compose/candidates`: budget, auth
/// health, and history with the typed recipient feed the score and reasons.
struct ComposeCandidate: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var email: String
    var name: String?
    var provider: String?
    var authState: String?
    var warmupActive: Bool?
    var dailyLimit: Int?
    var sentToday: Int?
    var remainingToday: Int?
    var historyMessages: Int?
    var lastContactAt: Date?
    var score: Int?
    var reasons: [String]?
    var recommended: Bool?

    enum CodingKeys: String, CodingKey {
        case id, email, name, provider, score, reasons, recommended
        case authState = "auth_state"
        case warmupActive = "warmup_active"
        case dailyLimit = "daily_limit"
        case sentToday = "sent_today"
        case remainingToday = "remaining_today"
        case historyMessages = "history_messages"
        case lastContactAt = "last_contact_at"
    }
}

/// Org-wide suppression state for the typed recipient (bounced, complained,
/// or unsubscribed); compose sends to a suppressed address are rejected.
struct ComposeSuppression: Codable, Sendable {
    var reason: String?
}

struct ComposeCandidatesResponse: Codable, Sendable {
    var accounts: [ComposeCandidate]?
    var recommendedAccountID: String?
    var recommendedReason: String?
    var contact: Contact?
    var suppression: ComposeSuppression?

    enum CodingKeys: String, CodingKey {
        case accounts, contact, suppression
        case recommendedAccountID = "recommended_account_id"
        case recommendedReason = "recommended_reason"
    }
}

// MARK: - Send

/// `POST /v1/unibox/compose`; empty `email_account_id` lets the backend pick
/// the best mailbox for the first recipient. With `from_tag_id` set, the
/// auto pick only considers mailboxes carrying that tag.
struct ComposeSendRequest: Encodable, Sendable {
    var emailAccountID: String
    var fromTagID: String?
    var to: [String]
    var cc: [String]?
    var bcc: [String]?
    var subject: String
    var bodyHTML: String?
    var bodyPlain: String?
    var sendMode: String?
    var scheduledAt: Date?

    enum CodingKeys: String, CodingKey {
        case to, cc, bcc, subject
        case emailAccountID = "email_account_id"
        case fromTagID = "from_tag_id"
        case bodyHTML = "body_html"
        case bodyPlain = "body_plain"
        case sendMode = "send_mode"
        case scheduledAt = "scheduled_at"
    }
}

struct ComposeSendResponse: Codable, Sendable {
    var taskID: String?
    var scheduledAt: Date?
    var sendMode: String?
    var accountID: String?
    var accountEmail: String?
    var auto: Bool?
    var pickedReason: String?

    enum CodingKeys: String, CodingKey {
        case auto
        case taskID = "task_id"
        case scheduledAt = "scheduled_at"
        case sendMode = "send_mode"
        case accountID = "account_id"
        case accountEmail = "account_email"
        case pickedReason = "picked_reason"
    }
}

// MARK: - Autosaved drafts

/// `repository.ComposeDraft` — a per-user working copy. The id is
/// client-generated, so autosave is an idempotent PUT of the whole draft.
struct ComposeDraft: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var emailAccountID: String?
    var to: [String]?
    var cc: [String]?
    var bcc: [String]?
    var subject: String?
    var body: String?
    var updatedAt: Date?
    var createdAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, to, cc, bcc, subject, body
        case emailAccountID = "email_account_id"
        case updatedAt = "updated_at"
        case createdAt = "created_at"
    }
}

struct ComposeDraftsPage: Codable, Sendable {
    var data: [ComposeDraft]?
}

/// `PUT /v1/unibox/drafts/:id` body.
struct ComposeDraftPayload: Encodable, Sendable, Equatable {
    var emailAccountID: String
    var to: [String]
    var cc: [String]
    var bcc: [String]
    var subject: String
    var body: String

    enum CodingKeys: String, CodingKey {
        case to, cc, bcc, subject, body
        case emailAccountID = "email_account_id"
    }

    var isEmpty: Bool {
        to.isEmpty && cc.isEmpty && bcc.isEmpty
            && subject.trimmingCharacters(in: .whitespaces).isEmpty
            && body.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

// MARK: - Grounded AI draft

/// `POST /v1/unibox/compose/draft` request; the server grounds the draft in
/// the recipient's contact record, correspondence history, and org voice.
struct ComposeAIDraftRequest: Encodable, Sendable {
    var to: String?
    var subject: String?
    var instruction: String?
}

struct ComposeGrounding: Codable, Sendable {
    var contact: Bool?
    var history: Int?
    var voiceProfile: Bool?

    enum CodingKeys: String, CodingKey {
        case contact, history
        case voiceProfile = "voice_profile"
    }
}

/// Exactly one of `text` / `question` is set: a ready draft, or a clarifying
/// question when the model genuinely cannot tell what the email is for.
struct ComposeAIDraftResponse: Codable, Sendable {
    var text: String?
    var question: String?
    var grounding: ComposeGrounding?
    var creditsRemaining: Int?
    var creditsCharged: Int?
    var tokensUsed: Int?
    var model: String?

    enum CodingKeys: String, CodingKey {
        case text, question, grounding, model
        case creditsRemaining = "credits_remaining"
        case creditsCharged = "credits_charged"
        case tokensUsed = "tokens_used"
    }

    /// "2 credits · 1.4k tok"; nil when nothing was charged (local model).
    var usage: String? {
        guard let creditsCharged, creditsCharged > 0 else { return nil }
        let credits = "\(creditsCharged) credit\(creditsCharged == 1 ? "" : "s")"
        guard let tokensUsed, tokensUsed > 0 else { return credits }
        return "\(credits) · \(WFormat.compact(tokensUsed)) tok"
    }

    /// "Grounded in contact · 6 past emails · voice profile".
    var groundingLine: String? {
        guard let grounding else { return nil }
        var parts: [String] = []
        if grounding.contact == true { parts.append("contact") }
        if let history = grounding.history, history > 0 {
            parts.append("\(history) past email\(history == 1 ? "" : "s")")
        }
        if grounding.voiceProfile == true { parts.append("voice profile") }
        guard !parts.isEmpty else { return nil }
        return "Grounded in " + parts.joined(separator: " · ")
    }
}
