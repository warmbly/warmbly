import Foundation

// Shared Codable models mirroring the Go json tags exactly.
// Convention: NO convertFromSnakeCase; every model declares explicit CodingKeys.

// MARK: - Tokens

struct AuthToken: Codable, Sendable {
    var accessToken: String
    var accessTokenExpiresAt: Date
    var refreshToken: String
    var refreshTokenExpiresAt: Date

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case accessTokenExpiresAt = "access_token_expires_at"
        case refreshToken = "refresh_token"
        case refreshTokenExpiresAt = "refresh_token_expires_at"
    }
}

/// `POST /auth/login/confirm` result: either the token pair or a 2FA challenge.
struct LoginResult: Codable, Sendable {
    var accessToken: String?
    var accessTokenExpiresAt: Date?
    var refreshToken: String?
    var refreshTokenExpiresAt: Date?
    var twoFARequired: Bool?
    var pendingToken: String?
    var expiresIn: Int?

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case accessTokenExpiresAt = "access_token_expires_at"
        case refreshToken = "refresh_token"
        case refreshTokenExpiresAt = "refresh_token_expires_at"
        case twoFARequired = "two_fa_required"
        case pendingToken = "pending_token"
        case expiresIn = "expires_in"
    }

    var token: AuthToken? {
        guard let accessToken, let accessTokenExpiresAt, let refreshToken, let refreshTokenExpiresAt else { return nil }
        return AuthToken(
            accessToken: accessToken,
            accessTokenExpiresAt: accessTokenExpiresAt,
            refreshToken: refreshToken,
            refreshTokenExpiresAt: refreshTokenExpiresAt
        )
    }
}

struct AuthSession: Codable, Sendable {
    var session: String
}

/// `GET /auth/providers`: which native social sign-in options this host
/// supports. One shipped binary adapts to hosted and self-hosted backends.
struct AuthProvidersInfo: Codable, Sendable {
    struct Provider: Codable, Sendable {
        var enabled: Bool
        var clientID: String?

        enum CodingKeys: String, CodingKey {
            case enabled
            case clientID = "client_id"
        }
    }

    var apple: Provider?
    var google: Provider?

    var appleEnabled: Bool { apple?.enabled == true }
    var googleClientID: String? {
        guard google?.enabled == true, let id = google?.clientID, !id.isEmpty else { return nil }
        return id
    }
}

// MARK: - Errors

struct APIErrorPayload: Codable, Sendable {
    var error: String?
    var message: String?
    var code: String?
    var requestId: String?
    var retryAfterMs: Int?

    enum CodingKeys: String, CodingKey {
        case error, message, code
        case requestId = "request_id"
        case retryAfterMs = "retry_after_ms"
    }
}

enum APIError: Error, LocalizedError {
    case notConfigured
    case unauthorized(APIErrorPayload?)
    case server(status: Int, payload: APIErrorPayload?)
    case rateLimited(retryAfterMs: Int?)
    case network(Error)
    case decoding(Error)

    var errorDescription: String? {
        switch self {
        case .notConfigured:
            return "Server address is not configured."
        case let .unauthorized(payload):
            return payload?.message ?? "Your session has expired. Please sign in again."
        case let .server(status, payload):
            return payload?.message ?? "The server returned an error (\(status))."
        case .rateLimited:
            return "Too many requests. Please wait a moment and try again."
        case .network:
            return "Couldn't reach the server. Check your connection."
        case .decoding:
            return "The server response couldn't be read."
        }
    }

    var code: String? {
        switch self {
        case let .unauthorized(payload): return payload?.code
        case let .server(_, payload): return payload?.code
        case .rateLimited: return "rate_limit_exceeded"
        default: return nil
        }
    }
}

// MARK: - Pagination

struct Pagination: Codable, Sendable {
    var total: Int64?
    var nextCursor: String?
    var hasMore: Bool?

    enum CodingKeys: String, CodingKey {
        case total
        case nextCursor = "next_cursor"
        case hasMore = "has_more"
    }
}

struct ListResponse<T: Codable & Sendable>: Codable, Sendable {
    var data: [T]
    var pagination: Pagination?
}

struct MessageResponse: Codable, Sendable {
    var message: String?
}

struct EmptyBody: Codable, Sendable {}

// MARK: - Permissions

/// `uint16` bitmask serialized as a JSON number; owner = 65535.
struct OrgPermissions: OptionSet, Sendable, Codable {
    let rawValue: UInt16

    static let manageTeam = OrgPermissions(rawValue: 1)
    static let manageBilling = OrgPermissions(rawValue: 2)
    static let manageCampaigns = OrgPermissions(rawValue: 4)
    static let manageContacts = OrgPermissions(rawValue: 8)
    static let manageEmails = OrgPermissions(rawValue: 16)
    static let viewAnalytics = OrgPermissions(rawValue: 32)
    static let sendCampaigns = OrgPermissions(rawValue: 64)
    static let accessUnibox = OrgPermissions(rawValue: 128)
    static let manageSequences = OrgPermissions(rawValue: 256)
    static let manageSettings = OrgPermissions(rawValue: 512)
    static let viewCampaigns = OrgPermissions(rawValue: 1024)
    static let viewContacts = OrgPermissions(rawValue: 2048)
    static let transferOwnership = OrgPermissions(rawValue: 4096)
    static let manageAPIKeys = OrgPermissions(rawValue: 8192)
    static let useIntegrations = OrgPermissions(rawValue: 16384)
    static let useAI = OrgPermissions(rawValue: 32768)

    static let owner = OrgPermissions(rawValue: .max)
}

// MARK: - User

/// Per-user label group (folders/tags/categories hydrated on GET /auth/me).
/// Go `models.Group` puts the display name on the wire as `title`.
struct UserGroup: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var name: String?
    var color: String?
    var position: Int?

    enum CodingKeys: String, CodingKey {
        case id, color, position
        case name = "title"
    }
}

struct User: Codable, Identifiable, Sendable {
    var id: String
    var firstName: String?
    var lastName: String?
    var email: String
    var avatarURL: String?
    var onboardingCompletedAt: Date?
    var maxOrganizations: Int?
    var freeTrialUsed: Bool?
    var isAdmin: Bool?
    var deletionScheduledFor: Date?
    var tags: [UserGroup]?
    var categories: [UserGroup]?
    var folders: [UserGroup]?

    enum CodingKeys: String, CodingKey {
        case id, email, tags, categories, folders
        case firstName = "first_name"
        case lastName = "last_name"
        case avatarURL = "avatar_url"
        case onboardingCompletedAt = "onboarding_completed_at"
        case maxOrganizations = "max_organizations"
        case freeTrialUsed = "free_trial_used"
        case isAdmin = "is_admin"
        case deletionScheduledFor = "deletion_scheduled_for"
    }

    var displayName: String {
        let name = [firstName, lastName].compactMap { $0 }.joined(separator: " ")
        return name.isEmpty ? email : name
    }

    var initials: String {
        let first = firstName?.first.map(String.init) ?? ""
        let last = lastName?.first.map(String.init) ?? ""
        let combined = first + last
        return combined.isEmpty ? String(email.prefix(1)).uppercased() : combined.uppercased()
    }
}

// MARK: - Organization

struct Organization: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var name: String
    var slug: String?
    var avatarURL: String?
    var ownerUserID: String?
    var createdAt: Date?
    var deletionScheduledFor: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, slug
        case avatarURL = "avatar_url"
        case ownerUserID = "owner_user_id"
        case createdAt = "created_at"
        case deletionScheduledFor = "deletion_scheduled_for"
    }
}

struct OrganizationMember: Codable, Identifiable, Sendable {
    var id: String
    var organizationID: String
    var userID: String
    var role: String?
    var permissions: UInt16?
    var invitedAt: Date?
    var acceptedAt: Date?
    var organization: Organization?

    enum CodingKeys: String, CodingKey {
        case id, role, permissions, organization
        case organizationID = "organization_id"
        case userID = "user_id"
        case invitedAt = "invited_at"
        case acceptedAt = "accepted_at"
    }

    var orgPermissions: OrgPermissions { OrgPermissions(rawValue: permissions ?? 0) }
}

struct OrgLimits: Codable, Sendable {
    var maxCampaigns: Int?
    var maxActiveCampaigns: Int?
    var maxTeamMembers: Int?
    var maxEmailAccounts: Int?
    var maxContacts: Int?
    var dailyCampaignLimit: Int?

    enum CodingKeys: String, CodingKey {
        case maxCampaigns = "max_campaigns"
        case maxActiveCampaigns = "max_active_campaigns"
        case maxTeamMembers = "max_team_members"
        case maxEmailAccounts = "max_email_accounts"
        case maxContacts = "max_contacts"
        case dailyCampaignLimit = "daily_campaign_limit"
    }
}

struct OrgCounts: Codable, Sendable {
    var totalCampaigns: Int?
    var activeCampaigns: Int?
    var totalContacts: Int?
    var totalMembers: Int?
    var emailAccounts: Int?

    enum CodingKeys: String, CodingKey {
        case totalCampaigns = "total_campaigns"
        case activeCampaigns = "active_campaigns"
        case totalContacts = "total_contacts"
        case totalMembers = "total_members"
        case emailAccounts = "email_accounts"
    }
}

struct OrganizationCurrent: Codable, Sendable {
    var id: String
    var name: String
    var slug: String?
    var avatarURL: String?
    var ownerUserID: String?
    var presenceShowOnline: Bool?
    var presenceShowActivity: Bool?
    var limits: OrgLimits?
    var counts: OrgCounts?

    enum CodingKeys: String, CodingKey {
        case id, name, slug, limits, counts
        case avatarURL = "avatar_url"
        case ownerUserID = "owner_user_id"
        case presenceShowOnline = "presence_show_online"
        case presenceShowActivity = "presence_show_activity"
    }
}

struct SwitchOrgResponse: Codable, Sendable {
    var message: String?
    var organizationID: String?

    enum CodingKeys: String, CodingKey {
        case message
        case organizationID = "organization_id"
    }
}

// MARK: - Realtime bootstrap

struct GetSocket: Codable, Sendable {
    var url: String
    /// Seconds; Go float64.
    var expiresIn: Double?

    enum CodingKeys: String, CodingKey {
        case url
        case expiresIn = "expires_in"
    }
}
