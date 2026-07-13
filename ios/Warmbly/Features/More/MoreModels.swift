import Foundation
import SwiftUI

// Codable models for the More tab. Field names mirror the Go json tags exactly;
// nothing here relies on snake_case conversion.

// MARK: - Team

/// Hydrated role chip on members and invitations (`roles: [{id,name,color}]`).
struct MoreRoleChip: Codable, Identifiable, Sendable, Hashable {
    var id: String
    var name: String?
    var color: String?

    enum CodingKeys: String, CodingKey {
        case id, name, color
    }
}

/// Row of `GET organization/members`.
struct TeamMember: Codable, Identifiable, Sendable {
    var id: String
    var organizationID: String?
    var userID: String
    var role: String?
    var roleID: String?
    var roles: [MoreRoleChip]?
    var permissions: UInt16?
    var invitedBy: String?
    var invitedAt: Date?
    var acceptedAt: Date?
    var user: User?
    var email: String?
    var name: String?

    enum CodingKeys: String, CodingKey {
        case id, role, roles, permissions, user, email, name
        case organizationID = "organization_id"
        case userID = "user_id"
        case roleID = "role_id"
        case invitedBy = "invited_by"
        case invitedAt = "invited_at"
        case acceptedAt = "accepted_at"
    }

    var isOwner: Bool { role == "owner" }

    var displayName: String {
        if let name, !name.isEmpty { return name }
        if let user { return user.displayName }
        return email ?? "Member"
    }

    var displayEmail: String {
        user?.email ?? email ?? ""
    }
}

/// Row of `GET organization/roles`.
struct TeamRole: Codable, Identifiable, Sendable {
    var id: String
    var organizationID: String?
    var name: String?
    var description: String?
    var color: String?
    var permissions: UInt16?
    var memberCount: Int?
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, description, color, permissions
        case organizationID = "organization_id"
        case memberCount = "member_count"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

/// Row of `GET organization/invitations`.
struct TeamInvitation: Codable, Identifiable, Sendable {
    var id: String
    var organizationID: String?
    var email: String?
    var role: String?
    var roleID: String?
    var roles: [MoreRoleChip]?
    var permissions: UInt16?
    var invitedBy: String?
    var expiresAt: Date?
    var createdAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, email, role, roles, permissions
        case organizationID = "organization_id"
        case roleID = "role_id"
        case invitedBy = "invited_by"
        case expiresAt = "expires_at"
        case createdAt = "created_at"
    }
}

struct MoreInviteBody: Encodable {
    var email: String
    var roleIDs: [String]

    enum CodingKeys: String, CodingKey {
        case email
        case roleIDs = "role_ids"
    }
}

struct MoreInviteResponse: Codable, Sendable {
    var message: String?
    var invitation: TeamInvitation?
}

// MARK: - Sessions and security

/// Element of the bare array returned by `GET auth/sessions`.
struct UserSession: Codable, Identifiable, Sendable {
    var id: String
    var current: Bool?
    var browser: String?
    var os: String?
    var locationCity: String?
    var locationRegion: String?
    var locationCountry: String?
    var countryCode: String?
    var authProvider: String?
    var createdAt: Date?
    var lastActiveAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, current, browser, os
        case locationCity = "location_city"
        case locationRegion = "location_region"
        case locationCountry = "location_country"
        case countryCode = "country_code"
        case authProvider = "auth_provider"
        case createdAt = "created_at"
        case lastActiveAt = "last_active_at"
    }
}

/// Element of the bare array returned by `GET auth/passkey/credentials`.
struct PasskeyCredential: Codable, Identifiable, Sendable {
    var id: String
    var name: String?
    var provider: String?
    var credentialID: String?
    var transports: [String]?
    var backupState: Bool?
    var createdAt: Date?
    var lastUsedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, provider, transports
        case credentialID = "credential_id"
        case backupState = "backup_state"
        case createdAt = "created_at"
        case lastUsedAt = "last_used_at"
    }
}

struct MoreTwoFAStatus: Codable, Sendable {
    var enabled: Bool?
}

struct MoreProfileBody: Encodable {
    var firstName: String
    var lastName: String

    enum CodingKeys: String, CodingKey {
        case firstName = "first_name"
        case lastName = "last_name"
    }
}

struct MorePasswordBody: Encodable {
    var currentPassword: String
    var newPassword: String

    enum CodingKeys: String, CodingKey {
        case currentPassword = "current_password"
        case newPassword = "new_password"
    }
}

// MARK: - Notifications

/// Element of `GET auth/me/notifications`.
struct UserNotification: Codable, Identifiable, Sendable {
    var id: String
    var userID: String?
    var organizationID: String?
    var category: String?
    var title: String?
    var body: String?
    var link: String?
    var readAt: Date?
    var createdAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, category, title, body, link
        case userID = "user_id"
        case organizationID = "organization_id"
        case readAt = "read_at"
        case createdAt = "created_at"
    }

    var isUnread: Bool { readAt == nil }
}

struct MoreNotificationFeed: Codable, Sendable {
    var notifications: [UserNotification]?
    var unread: Int?
}

struct MoreOkResponse: Codable, Sendable {
    var ok: Bool?
}

struct MoreChannelPrefs: Codable, Sendable {
    var inApp: Bool?
    var email: Bool?
    var slack: Bool?
    var push: Bool?

    enum CodingKeys: String, CodingKey {
        case email, slack, push
        case inApp = "in_app"
    }
}

struct MoreCategoryPref: Codable, Sendable {
    var enabled: Bool?
    var channels: MoreChannelPrefs?

    enum CodingKeys: String, CodingKey {
        case enabled, channels
    }
}

/// The preferences object keyed by category string; decoded dynamically so new
/// backend categories render without an app update.
struct NotificationPreferences: Codable, Sendable {
    var categories: [String: MoreCategoryPref]

    init(categories: [String: MoreCategoryPref] = [:]) {
        self.categories = categories
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        categories = try container.decode([String: MoreCategoryPref].self)
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(categories)
    }
}

struct MoreNotificationPreferencesEnvelope: Codable, Sendable {
    var preferences: NotificationPreferences?
}

struct MorePreferencesBody: Encodable {
    var preferences: NotificationPreferences
}

// MARK: - Billing

struct MorePlan: Codable, Sendable {
    var id: String?
    var name: String?
    var price: Double?
    var discountedPrice: Double?
    var duration: String?
    var dailyEmails: Int?
    var maxContacts: Int?
    var accountLimit: Int?

    enum CodingKeys: String, CodingKey {
        case id, name, price, duration
        case discountedPrice = "discounted_price"
        case dailyEmails = "daily_emails"
        case maxContacts = "max_contacts"
        case accountLimit = "account_limit"
    }
}

/// `GET subscription`.
struct SubscriptionInfo: Codable, Sendable {
    var id: String?
    var organizationID: String?
    var planID: String?
    var status: String?
    var currentPeriodStart: Date?
    var currentPeriodEnd: Date?
    var cancelAtPeriodEnd: Bool?
    var canceledAt: Date?
    var trialStart: Date?
    var trialEnd: Date?
    var freeTrialEndsAt: Date?
    var isEnterprise: Bool?
    var plan: MorePlan?

    enum CodingKeys: String, CodingKey {
        case id, status, plan
        case organizationID = "organization_id"
        case planID = "plan_id"
        case currentPeriodStart = "current_period_start"
        case currentPeriodEnd = "current_period_end"
        case cancelAtPeriodEnd = "cancel_at_period_end"
        case canceledAt = "canceled_at"
        case trialStart = "trial_start"
        case trialEnd = "trial_end"
        case freeTrialEndsAt = "free_trial_ends_at"
        case isEnterprise = "is_enterprise"
    }
}

/// `GET subscription/trial`.
struct TrialInfo: Codable, Sendable {
    var isInTrial: Bool?
    var trialEndsAt: Date?
    var daysRemaining: Int?
    var isExpired: Bool?
    var isSubscribed: Bool?

    enum CodingKeys: String, CodingKey {
        case isInTrial = "is_in_trial"
        case trialEndsAt = "trial_ends_at"
        case daysRemaining = "days_remaining"
        case isExpired = "is_expired"
        case isSubscribed = "is_subscribed"
    }
}

struct MoreRateLimits: Codable, Sendable {
    var wsMessagePerMin: Int?
    var wsJoinPerMin: Int?
    var wsEventPerMin: Int?
    var maxConnections: Int?

    enum CodingKeys: String, CodingKey {
        case wsMessagePerMin = "limit_ws_message_pm"
        case wsJoinPerMin = "limit_ws_join_pm"
        case wsEventPerMin = "limit_ws_event_pm"
        case maxConnections = "max_connections"
    }
}

/// `GET subscription/limits`; we only read the rate-limit block.
struct MoreSubscriptionLimits: Codable, Sendable {
    var rateLimits: MoreRateLimits?

    enum CodingKeys: String, CodingKey {
        case rateLimits = "rate_limits"
    }
}

// MARK: - Flat list language

/// Shared metrics for the flat account screens (mirrors CampaignDetailView).
enum MoreFlatMetrics {
    /// Hairline leading for IconTile(34) rows: 34 tile + 12 gap + 16.
    static let tileTextLeading: CGFloat = 62
    /// Hairline leading for WAvatar(40) rows: 40 avatar + 12 gap + 16.
    static let avatarTextLeading: CGFloat = 68
}

/// Eyebrow caption as a bare row, never a grouped `Section` header (that
/// reintroduces the sticky gray band). Whitespace above is the separator.
struct MoreFlatSectionHeader: View {
    let title: String
    var top: CGFloat = 20

    init(_ title: String, top: CGFloat = 20) {
        self.title = title
        self.top = top
    }

    var body: some View {
        EyebrowLabel(title)
            .padding(.horizontal, 20)
            .padding(.top, top)
            .padding(.bottom, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .listRowInsets(EdgeInsets())
            .listRowSeparator(.hidden)
            .listRowBackground(Color(.systemBackground))
    }
}

extension View {
    /// Flat full-bleed row; the hairline tucks under the text column when
    /// `textLeading` is given, Gmail-style.
    func moreFlatRow(separator: Visibility = .automatic, textLeading: CGFloat? = nil) -> some View {
        listRowInsets(EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20))
            .listRowSeparator(separator)
            .alignmentGuide(.listRowSeparatorLeading) { dims in
                textLeading ?? dims[.listRowSeparatorLeading]
            }
            .listRowBackground(Color(.systemBackground))
    }
}

// MARK: - Styling helpers

enum MoreStyle {
    /// Parses a `#rrggbb` role/label color string.
    static func color(hex string: String?) -> Color? {
        guard var s = string?.trimmingCharacters(in: .whitespaces), !s.isEmpty else { return nil }
        if s.hasPrefix("#") { s.removeFirst() }
        guard s.count == 6, let value = UInt32(s, radix: 16) else { return nil }
        return Color(hex: value)
    }

    /// Plan pill palette: free slate, starter emerald, grow amber,
    /// business indigo, enterprise violet.
    static func planColors(_ name: String) -> (fg: Color, bg: Color) {
        switch name.lowercased() {
        case "free":
            return (Tone.slate.color, Tone.slate.background)
        case "starter":
            return (Tone.emerald.color, Tone.emerald.background)
        case "grow":
            return (Tone.amber.color, Tone.amber.background)
        case "business":
            return (Tone.indigo.color, Tone.indigo.background)
        case "enterprise":
            return (
                Color.dynamic(light: 0x6D28D9, dark: 0xC4B5FD),
                Color.dynamic(light: 0xF5F3FF, dark: 0x4C1D95)
            )
        default:
            return (Tone.sky.color, Tone.sky.background)
        }
    }
}
