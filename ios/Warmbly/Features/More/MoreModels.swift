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
/// backend categories render without an app update. `email_digest_minutes` is
/// a sibling scalar in the same object, so it is split out of the category
/// map (30 minute floor, 24h ceiling, mirroring the backend bounds).
struct NotificationPreferences: Codable, Sendable {
    var categories: [String: MoreCategoryPref]
    var emailDigestMinutes: Int

    static let emailWindowKey = "email_digest_minutes"
    static let emailWindowMin = 30
    static let emailWindowMax = 1440

    init(categories: [String: MoreCategoryPref] = [:], emailDigestMinutes: Int = 30) {
        self.categories = categories
        self.emailDigestMinutes = emailDigestMinutes
    }

    private struct DynamicKey: CodingKey {
        var stringValue: String
        var intValue: Int? { nil }
        init(stringValue: String) { self.stringValue = stringValue }
        init?(intValue: Int) { nil }
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: DynamicKey.self)
        var decoded: [String: MoreCategoryPref] = [:]
        var window = Self.emailWindowMin
        for key in container.allKeys {
            if key.stringValue == Self.emailWindowKey {
                let raw = (try? container.decode(Int.self, forKey: key)) ?? Self.emailWindowMin
                window = min(max(raw, Self.emailWindowMin), Self.emailWindowMax)
            } else if let pref = try? container.decode(MoreCategoryPref.self, forKey: key) {
                decoded[key.stringValue] = pref
            }
        }
        // Older servers omit these categories; seed the backend defaults.
        if decoded["billing_alert"] == nil {
            decoded["billing_alert"] = MoreCategoryPref(
                enabled: true,
                channels: MoreChannelPrefs(inApp: true, email: true, slack: false, push: true)
            )
        }
        if decoded["team_activity"] == nil {
            decoded["team_activity"] = MoreCategoryPref(
                enabled: true,
                channels: MoreChannelPrefs(inApp: true, email: false, slack: false, push: true)
            )
        }
        categories = decoded
        emailDigestMinutes = window
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: DynamicKey.self)
        for (key, pref) in categories {
            try container.encode(pref, forKey: DynamicKey(stringValue: key))
        }
        try container.encode(emailDigestMinutes, forKey: DynamicKey(stringValue: Self.emailWindowKey))
    }
}

struct MoreNotificationPreferencesEnvelope: Codable, Sendable {
    var preferences: NotificationPreferences?
    var emailDelivery: MoreEmailDeliveryInfo?

    enum CodingKeys: String, CodingKey {
        case preferences
        case emailDelivery = "email_delivery"
    }
}

/// Email-channel bounds from the deployment: the window range clients should
/// offer, and the rolling 24h per-user email budget (0 = unlimited).
struct MoreEmailDeliveryInfo: Codable, Sendable {
    var minMinutes: Int?
    var maxMinutes: Int?
    var dailyCap: Int?

    enum CodingKeys: String, CodingKey {
        case minMinutes = "min_minutes"
        case maxMinutes = "max_minutes"
        case dailyCap = "daily_cap"
    }
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
    var monthlyCredits: Int?
    /// Usage included each month in dollars; nil from a backend before the balance.
    var monthlyAllowanceUSD: Double?
    var dedicatedWorkers: Int?
    var savings: Double?
    var isPublic: Bool?

    enum CodingKeys: String, CodingKey {
        case id, name, price, duration, savings
        case discountedPrice = "discounted_price"
        case dailyEmails = "daily_emails"
        case maxContacts = "max_contacts"
        case accountLimit = "account_limit"
        case monthlyCredits = "monthly_credits"
        case monthlyAllowanceUSD = "monthly_allowance_usd"
        case dedicatedWorkers = "dedicated_workers"
        case isPublic = "public"
    }
}

/// `GET plans`.
struct PlansResponse: Codable, Sendable {
    var plans: [MorePlan]?
}

/// `GET organization/current/limits`: what the server enforces, and the counts against it.
struct OrgUsageSnapshot: Codable, Sendable {
    var counts: OrgCounts?
    var limits: OrgLimits?
    var mailboxes: Mailboxes?
    var storage: Storage?

    struct Mailboxes: Codable, Sendable {
        var used: Int?
        var allowance: Int?
    }

    struct Storage: Codable, Sendable {
        var limitBytes: Int64?
        var usedBytes: Int64?

        enum CodingKeys: String, CodingKey {
            case limitBytes = "limit_bytes"
            case usedBytes = "used_bytes"
        }
    }
}

/// `GET subscription/credits`. The `*USD` fields are the dollar balance; a
/// backend from before it sends only the legacy credit figures.
struct CreditBalance: Codable, Sendable {
    var balance: Int?
    var monthlyAllowance: Int?
    var monthlyBalance: Int?
    var purchasedBalance: Int?
    var nextResetAt: Date?
    var unlimited: Bool?
    var balanceUSD: Double?
    var monthlyUSD: Double?
    var purchasedUSD: Double?
    var monthlyAllowanceUSD: Double?

    enum CodingKeys: String, CodingKey {
        case balance, unlimited
        case monthlyAllowance = "monthly_allowance"
        case monthlyBalance = "monthly_balance"
        case purchasedBalance = "purchased_balance"
        case nextResetAt = "next_reset_at"
        case balanceUSD = "balance_usd"
        case monthlyUSD = "monthly_usd"
        case purchasedUSD = "purchased_usd"
        case monthlyAllowanceUSD = "monthly_allowance_usd"
    }
}

/// `GET subscription/credits/usage`, in dollars when the backend sends them.
struct CreditUsage: Codable, Sendable {
    var spentToday: Int?
    var spentWeek: Int?
    var spentMonth: Int?
    var spentTodayUSD: Double?
    var spentWeekUSD: Double?
    var spentMonthUSD: Double?
    var series: [Point]?
    var byReason: [Bucket]?

    struct Point: Codable, Sendable {
        var date: String
        var credits: Int?
        var amountUSD: Double?

        enum CodingKeys: String, CodingKey {
            case date, credits
            case amountUSD = "amount_usd"
        }
    }

    struct Bucket: Codable, Sendable {
        var key: String
        var credits: Int?
        var amountUSD: Double?
        var count: Int?

        enum CodingKeys: String, CodingKey {
            case key, credits, count
            case amountUSD = "amount_usd"
        }
    }

    enum CodingKeys: String, CodingKey {
        case series
        case spentToday = "spent_today"
        case spentWeek = "spent_week"
        case spentMonth = "spent_month"
        case spentTodayUSD = "spent_today_usd"
        case spentWeekUSD = "spent_week_usd"
        case spentMonthUSD = "spent_month_usd"
        case byReason = "by_reason"
    }
}

/// `GET subscription/balance`: read here only for the automatic reload state,
/// which is changed on the web. A backend from before the balance has no route.
struct BalanceInfo: Codable, Sendable {
    var unlimited: Bool?
    var autoReload: AutoReload?

    struct AutoReload: Codable, Sendable {
        var enabled: Bool?
        var amountUSD: Double?
        var thresholdUSD: Double?

        enum CodingKeys: String, CodingKey {
            case enabled
            case amountUSD = "amount_usd"
            case thresholdUSD = "threshold_usd"
        }
    }

    enum CodingKeys: String, CodingKey {
        case unlimited
        case autoReload = "auto_reload"
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

// MARK: - Styling helpers

enum MoreStyle {
    /// Parses a `#rrggbb` role/label color string.
    static func color(hex string: String?) -> Color? {
        guard var s = string?.trimmingCharacters(in: .whitespaces), !s.isEmpty else { return nil }
        if s.hasPrefix("#") { s.removeFirst() }
        guard s.count == 6, let value = UInt32(s, radix: 16) else { return nil }
        return Color(hex: value)
    }
}
