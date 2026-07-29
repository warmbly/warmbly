import Foundation

// MARK: - Sessions

/// `models.PendingAgentTool` — a risky tool paused for human approval.
struct AgentPendingTool: Codable, Sendable, Hashable {
    var messageID: String?
    var toolCallID: String?
    var toolName: String?
    var risk: String?
    var argsSummary: String?

    enum CodingKeys: String, CodingKey {
        case risk
        case messageID = "message_id"
        case toolCallID = "tool_call_id"
        case toolName = "tool_name"
        case argsSummary = "args_summary"
    }
}

struct AgentSessionContext: Codable, Sendable, Hashable {
    var page: String?
    var resource: String?
    var model: String?
    var freeModel: Bool?
    var pending: AgentPendingTool?

    enum CodingKeys: String, CodingKey {
        case page, resource, model, pending
        case freeModel = "free_model"
    }
}

/// `models.AgentSession` — one assistant conversation. `userName` is present
/// only in workspaces with shared assistant history enabled.
struct AgentSession: Codable, Identifiable, Sendable {
    var id: String
    var orgID: String?
    var userID: String?
    var userName: String?
    var title: String?
    var context: AgentSessionContext?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, title, context
        case orgID = "org_id"
        case userID = "user_id"
        case userName = "user_name"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    var displayTitle: String {
        let trimmed = (title ?? "").trimmingCharacters(in: .whitespaces)
        return trimmed.isEmpty ? "Untitled" : trimmed
    }
}

extension AgentSession: Hashable {
    static func == (lhs: AgentSession, rhs: AgentSession) -> Bool {
        lhs.id == rhs.id && lhs.updatedAt == rhs.updatedAt
    }

    func hash(into hasher: inout Hasher) {
        hasher.combine(id)
        hasher.combine(updatedAt)
    }
}

struct AgentSessionsPage: Codable, Sendable {
    var data: [AgentSession]?
    var pagination: Pagination?
}

struct AgentSessionCreateRequest: Encodable, Sendable {
    var page: String?
    var resource: String?
}

// MARK: - Transcript (GET /ai/sessions/:id/messages)

/// `aiagent.HydratedBlock` — one text passage or tool step within a turn.
struct AgentTranscriptBlock: Codable, Sendable {
    var kind: String?
    var text: String?
    var tool: String?
    var argsSummary: String?
    var result: String?
    var entityType: String?
    var entityID: String?
    var openURL: String?
    var done: Bool?

    enum CodingKeys: String, CodingKey {
        case kind, text, tool, result, done
        case argsSummary = "args_summary"
        case entityType = "entity_type"
        case entityID = "entity_id"
        case openURL = "open_url"
    }
}

struct AgentTranscriptTurn: Codable, Sendable {
    var role: String?
    var blocks: [AgentTranscriptBlock]?
}

struct AgentTranscriptResponse: Codable, Sendable {
    var title: String?
    var turns: [AgentTranscriptTurn]?
    var pending: AgentPendingTool?
    var freeModel: Bool?

    enum CodingKeys: String, CodingKey {
        case title, turns, pending
        case freeModel = "free_model"
    }
}

// MARK: - SSE stream

/// `aiagent.StreamEvent` — one `data:` frame of the message/approve stream.
/// `type` is one of: text, text_delta, tool_start, tool_result,
/// approval_required, iteration, done, error.
struct AgentStreamEvent: Codable, Sendable {
    var type: String
    var text: String?
    var tool: String?
    var risk: String?
    var argsSummary: String?
    var toolCallID: String?
    var result: String?
    var iteration: Int?
    var creditsRemaining: Int?
    var budget: Int?
    var freeModel: Bool?
    var code: String?
    var message: String?
    var entityType: String?
    var entityID: String?
    var openURL: String?

    enum CodingKeys: String, CodingKey {
        case type, text, tool, risk, result, iteration, budget, code, message
        case argsSummary = "args_summary"
        case toolCallID = "tool_call_id"
        case creditsRemaining = "credits_remaining"
        case freeModel = "free_model"
        case entityType = "entity_type"
        case entityID = "entity_id"
        case openURL = "open_url"
    }
}

struct AgentMessageRequest: Encodable, Sendable {
    var messageID: String
    var text: String
    var page: String?
    var resource: String?

    enum CodingKeys: String, CodingKey {
        case text, page, resource
        case messageID = "message_id"
    }
}

struct AgentApproveRequest: Encodable, Sendable {
    var decision: String
}

// MARK: - Display helpers

enum AgentToolDisplay {
    /// "create_campaign_draft" -> "Create campaign draft".
    static func label(_ tool: String?) -> String {
        guard let tool, !tool.isEmpty else { return "Tool" }
        let words = tool.split(separator: "_").map(String.init)
        guard let first = words.first else { return tool }
        return ([first.capitalized] + words.dropFirst()).joined(separator: " ")
    }
}
