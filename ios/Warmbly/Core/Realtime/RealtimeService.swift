import Foundation
import os

/// Cache-invalidation domains. Views watch `realtime.pulse(for:)` and reload
/// when the counter bumps — the iOS analog of the web's AUDIT_CREATED spine.
enum RealtimeDomain: String, CaseIterable, Sendable {
    case unibox, campaigns, contacts, emailAccounts, analytics, team
    case templates, crm, meetings, automations, integrations, apiKeys
    case settings, notifications, audit, billing, me
}

// MARK: - Presence

struct PresenceMeta: Identifiable, Sendable {
    var id: String { phxRef ?? UUID().uuidString }
    var phxRef: String?
    var name: String?
    var avatar: String?
    var page: String?
    var resource: String?
    var action: String?
    var onlineAt: Double?
    var updatedAt: Double?

    static func parse(_ dict: [String: Any]) -> PresenceMeta {
        PresenceMeta(
            phxRef: dict["phx_ref"] as? String,
            name: dict["name"] as? String,
            avatar: dict["avatar"] as? String,
            page: dict["page"] as? String,
            resource: dict["resource"] as? String,
            action: dict["action"] as? String,
            onlineAt: dict["online_at"] as? Double,
            updatedAt: dict["updated_at"] as? Double
        )
    }
}

struct PresenceMember: Identifiable, Sendable {
    var id: String
    var metas: [PresenceMeta]

    /// The most informative meta (an active editing/replying one wins).
    var primary: PresenceMeta? {
        metas.first { $0.action == "editing" || $0.action == "replying" } ?? metas.first
    }
}

@MainActor
@Observable
final class PresenceStore {
    private(set) var members: [String: [PresenceMeta]] = [:]

    var online: [PresenceMember] {
        members
            .filter { !$0.value.isEmpty }
            .map { PresenceMember(id: $0.key, metas: $0.value) }
            .sorted { ($0.primary?.name ?? $0.id) < ($1.primary?.name ?? $1.id) }
    }

    var onlineCount: Int { members.values.filter { !$0.isEmpty }.count }

    /// Members currently on a given resource (excluding a user id, usually self).
    func viewers(of resource: String, excluding userID: String?) -> [PresenceMember] {
        online.compactMap { member in
            guard member.id != userID else { return nil }
            let matching = member.metas.filter { $0.resource == resource }
            guard !matching.isEmpty else { return nil }
            return PresenceMember(id: member.id, metas: matching)
        }
    }

    func applyState(_ payload: [String: Any]) {
        var next: [String: [PresenceMeta]] = [:]
        for (userID, entry) in payload {
            guard let entry = entry as? [String: Any],
                  let metas = entry["metas"] as? [[String: Any]]
            else { continue }
            next[userID] = metas.map(PresenceMeta.parse)
        }
        members = next
    }

    func applyDiff(_ payload: [String: Any]) {
        var next = members
        if let leaves = payload["leaves"] as? [String: Any] {
            for (userID, entry) in leaves {
                guard let entry = entry as? [String: Any],
                      let metas = entry["metas"] as? [[String: Any]]
                else { continue }
                let leftRefs = Set(metas.compactMap { $0["phx_ref"] as? String })
                next[userID] = (next[userID] ?? []).filter { meta in
                    guard let ref = meta.phxRef else { return false }
                    return !leftRefs.contains(ref)
                }
                if next[userID]?.isEmpty == true { next.removeValue(forKey: userID) }
            }
        }
        if let joins = payload["joins"] as? [String: Any] {
            for (userID, entry) in joins {
                guard let entry = entry as? [String: Any],
                      let metas = entry["metas"] as? [[String: Any]]
                else { continue }
                let incoming = metas.map(PresenceMeta.parse)
                let incomingRefs = Set(incoming.compactMap(\.phxRef))
                var kept = (next[userID] ?? []).filter { meta in
                    guard let ref = meta.phxRef else { return false }
                    return !incomingRefs.contains(ref)
                }
                kept.append(contentsOf: incoming)
                next[userID] = kept
            }
        }
        members = next
    }

    func reset() { members = [:] }
}

/// Thread-safe holder for the resume sequence (read from socket threads).
private final class SeqBox: @unchecked Sendable {
    private let lock = NSLock()
    private var seq: Int?

    var value: Int? {
        get { lock.lock(); defer { lock.unlock() }; return seq }
        set { lock.lock(); seq = newValue; lock.unlock() }
    }
}

// MARK: - Realtime service

@MainActor
@Observable
final class RealtimeService {
    enum ConnectionState { case disconnected, connecting, connected }

    let presence = PresenceStore()
    private(set) var connectionState: ConnectionState = .disconnected
    private(set) var pulses: [RealtimeDomain: Int] = [:]

    private let api: APIClient
    private let socket = PhoenixSocket()
    private let log = Logger(subsystem: "com.warmbly.app", category: "realtime")

    private var userID: String?
    private var orgID: String?
    private let seqBox = SeqBox()
    private var lastSeq: Int? {
        get { seqBox.value }
        set { seqBox.value = newValue }
    }

    private var hadSession = false
    private var currentPresence: (page: String?, resource: String?, action: String?) = (nil, nil, nil)
    private var lastPresencePush = Date.distantPast

    init(api: APIClient) {
        self.api = api

        socket.urlProvider = { [api] in
            guard let bootstrap: GetSocket = try? await api.post("getaway"),
                  var components = URLComponents(string: bootstrap.url)
            else { return nil }
            var items = components.queryItems ?? []
            if !items.contains(where: { $0.name == "vsn" }) {
                items.append(URLQueryItem(name: "vsn", value: "1.0.0"))
            }
            components.queryItems = items
            // Advanced override: swap only the origin, keeping the signed
            // path/token the backend minted.
            let override = AppConfig.realtimeOriginOverride.trimmingCharacters(in: .whitespaces)
            if !override.isEmpty, let originURL = URL(string: override), let host = originURL.host {
                components.host = host
                components.port = originURL.port
                if let scheme = originURL.scheme {
                    components.scheme = scheme == "https" ? "wss" : scheme == "http" ? "ws" : scheme
                }
            }
            return components.url
        }
        socket.joinPayloadProvider = { [seqBox] topic in
            // Runs on socket threads; only touch the lock-protected seq box.
            if topic.hasPrefix("org:"), let seq = seqBox.value {
                return ["resume": ["last_seq": seq]]
            }
            return [:]
        }
        socket.onState = { [weak self] state in
            Task { @MainActor in self?.handleSocketState(state) }
        }
        socket.onEvent = { [weak self] message in
            let event = message.event
            let payload = message.payload
            Task { @MainActor in self?.handleEvent(event, payload) }
        }
        socket.onJoin = { [weak self] topic, ok, response in
            Task { @MainActor in self?.handleJoin(topic: topic, ok: ok, response: response) }
        }
    }

    // MARK: Lifecycle

    func connect(userID: String, orgID: String) {
        self.userID = userID
        if let previous = self.orgID, previous != orgID {
            socket.leave("org:\(previous)")
            presence.reset()
            lastSeq = nil
        }
        self.orgID = orgID
        socket.join("user:\(userID)")
        socket.join("org:\(orgID)")
        socket.connect()
    }

    /// Foreground kick: recheck the socket immediately instead of letting the
    /// heartbeat watchdog discover a suspension-killed connection ~30s late.
    func nudge() {
        guard userID != nil else { return }
        socket.nudge()
    }

    func disconnect() {
        socket.disconnect()
        presence.reset()
        userID = nil
        orgID = nil
        lastSeq = nil
        hadSession = false
        connectionState = .disconnected
    }

    private func handleSocketState(_ state: PhoenixSocket.State) {
        switch state {
        case .disconnected: connectionState = .disconnected
        case .connecting: connectionState = .connecting
        case .connected:
            connectionState = .connected
            if hadSession {
                // Events missed while down may be gone; refresh everything.
                bumpAll()
            }
            hadSession = true
        }
    }

    private func handleJoin(topic: String, ok: Bool, response: [String: Any]) {
        guard topic.hasPrefix("org:") else { return }
        if ok {
            if lastSeq == nil, let seq = response["seq"] as? Int { lastSeq = seq }
            pushCurrentPresence(force: true)
        } else {
            log.warning("org join failed: \(String(describing: response))")
        }
    }

    // MARK: Invalidation

    func pulse(for domain: RealtimeDomain) -> Int {
        pulses[domain] ?? 0
    }

    private func bump(_ domains: [RealtimeDomain]) {
        for domain in domains {
            pulses[domain] = (pulses[domain] ?? 0) + 1
        }
    }

    private func bumpAll() {
        bump(RealtimeDomain.allCases)
    }

    private func handleEvent(_ event: String, _ payload: [String: Any]) {
        if let seq = payload["seq"] as? Int {
            if let last = lastSeq, seq <= last { return }
            lastSeq = seq
        }

        switch event {
        case "presence_state":
            presence.applyState(payload)
            pushCurrentPresence(force: true)
            return
        case "presence_diff":
            presence.applyDiff(payload)
            return
        case "rate_limited":
            return
        case "resumed":
            return
        case "resume_failed":
            if let seq = payload["current_seq"] as? Int { lastSeq = seq }
            bumpAll()
            return
        default:
            break
        }

        let name = normalized(event)
        if name.hasPrefix("PRESENCE") || name.hasPrefix("LIVE_") || name == "RATE_LIMITED" { return }

        if name == "AUDIT_CREATED" {
            bump([.audit])
            if let entityType = payload["entity_type"] as? String {
                bump(Self.spine[entityType] ?? [])
            }
            return
        }
        bump(Self.domains(forEventName: name))
    }

    private func normalized(_ raw: String) -> String {
        raw.uppercased()
            .map { ".:- \t".contains($0) ? "_" : $0 }
            .reduce(into: "") { partial, ch in
                if ch == "_", partial.hasSuffix("_") { return }
                partial.append(ch)
            }
    }

    /// entity_type -> domains, mirroring the web's spine map.
    private static let spine: [String: [RealtimeDomain]] = [
        "contact": [.contacts],
        "campaign": [.campaigns, .analytics],
        "step": [.campaigns],
        "email_account": [.emailAccounts, .analytics],
        "api_key": [.apiKeys],
        "webhook": [.settings, .integrations],
        "template": [.templates],
        "organization": [.team, .settings],
        "organization_member": [.team],
        "invitation": [.team],
        "team": [.team],
        "role": [.team],
        "automation": [.automations],
        "integration": [.integrations],
        "lead_sync_source": [.integrations, .contacts],
        "meeting": [.meetings, .crm],
        "subscription": [.billing],
        "referral": [.billing],
        "referral_credit": [.billing],
        "settings": [.settings],
        "unibox": [.unibox],
        "crm_note": [.crm, .contacts],
        "crm_pipeline": [.crm],
        "crm_stage": [.crm],
        "crm_deal": [.crm],
        "crm_task": [.crm],
        "warmup_routing_rule": [.emailAccounts, .analytics],
        "folder": [.me, .campaigns],
        "tag": [.me, .emailAccounts, .unibox],
        "category": [.me, .contacts],
    ]

    private static func domains(forEventName name: String) -> [RealtimeDomain] {
        if name.contains("NOTIFICATION") { return [.notifications] }
        if name.contains("INBOX") || name == "EMAIL_RECEIVED" {
            return [.unibox, .analytics, .emailAccounts]
        }
        if name == "EMAIL_UPDATED" || name == "EMAIL_DELETED" { return [.unibox, .analytics] }
        if name == "EMAIL_REPLIED" { return [.campaigns, .analytics, .unibox] }
        if name == "EMAIL_SENT" || name == "EMAIL_OPENED" || name == "EMAIL_CLICKED"
            || name == "EMAIL_FAILED" || name == "EMAIL_BOUNCED" || name.contains("TASK_PROGRESS") {
            return [.campaigns, .analytics]
        }
        if name == "EMAIL_STATUS" || name == "EMAIL_ERROR" { return [.emailAccounts, .analytics] }
        if name.contains("CAMPAIGN") { return [.campaigns, .analytics] }
        if name.contains("CONTACT") { return [.contacts, .campaigns, .analytics] }
        if name.contains("ACCOUNT") || name.contains("WARMUP") { return [.emailAccounts, .analytics] }
        if name.contains("BULK") { return [.contacts] }
        if name.contains("MEETING") || name.contains("BOOKING") { return [.meetings, .crm] }
        if name.contains("DEAL") { return [.crm, .contacts] }
        if name.contains("SUBSCRIPTION") || name.contains("PLAN") || name.contains("BILLING") || name.contains("LIMIT") {
            return [.billing, .me]
        }
        if name.contains("AUTOMATION") { return [.automations] }
        if name.contains("TASK") { return [.campaigns] }
        return []
    }

    // MARK: Presence pushes

    /// Report what the user is looking at; throttled to respect the 60/min
    /// ws_event budget.
    func setPresence(page: String?, resource: String?, action: String?) {
        currentPresence = (page, resource, action)
        pushCurrentPresence(force: false)
    }

    private func pushCurrentPresence(force: Bool) {
        guard connectionState == .connected, let orgID else { return }
        if !force, Date().timeIntervalSince(lastPresencePush) < 1.5 { return }
        lastPresencePush = Date()
        var payload: [String: Any] = [:]
        payload["page"] = currentPresence.page ?? NSNull()
        payload["resource"] = currentPresence.resource ?? NSNull()
        payload["action"] = currentPresence.action ?? NSNull()
        socket.push(topic: "org:\(orgID)", event: "presence:update", payload: payload)
    }
}
