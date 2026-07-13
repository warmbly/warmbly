import Foundation
import os

struct PhoenixMessage {
    let topic: String
    let event: String
    let payload: [String: Any]
    let ref: String?
}

/// Phoenix Channels client over URLSessionWebSocketTask.
///
/// Speaks the **V1 serializer** (`vsn=1.0.0`, JSON *object* frames
/// `{topic, event, payload, ref}`) — the only wire format the Warmbly
/// realtime service is known-good with; V2 array frames crashed it for the
/// web client. Heartbeat every 25s with an 8s reply watchdog; reconnect
/// backoff [120, 350, 800, 1500, 3000, 5000, 10000] ms with ±25% jitter.
final class PhoenixSocket: NSObject, URLSessionWebSocketDelegate, @unchecked Sendable {
    enum State: Sendable {
        case disconnected
        case connecting
        case connected
    }

    /// Fetches a fresh socket URL before every (re)connect. The token inside
    /// is single-session and expires in 10 minutes, so it can't be reused.
    // Non-optional on purpose: an optional async closure property crashes
    // the Xcode 26.6 type checker at the use site (fixed in 27). The nil
    // default reads as "not configured yet".
    var urlProvider: () async -> URL? = { nil }
    /// Join payload per topic, asked on every join/rejoin (carries resume state).
    var joinPayloadProvider: ((String) -> [String: Any])?
    var onEvent: ((PhoenixMessage) -> Void)?
    var onJoin: ((_ topic: String, _ ok: Bool, _ response: [String: Any]) -> Void)?
    var onState: ((State) -> Void)?

    private let log = Logger(subsystem: "com.warmbly.app", category: "realtime")
    private let lock = NSLock()

    private var session: URLSession?
    private var task: URLSessionWebSocketTask?
    private var shouldRun = false
    private var refCounter = 0
    private var reconnectAttempt = 0
    private var topics: Set<String> = []
    private var joinRefs: [String: String] = [:]
    private var pendingReplies: [String: (Bool, [String: Any]) -> Void] = [:]
    private var heartbeatTask: Task<Void, Never>?
    private var heartbeatAwaitingRef: String?

    private static let backoffMs: [Double] = [120, 350, 800, 1500, 3000, 5000, 10000]

    private(set) var state: State = .disconnected {
        didSet { if state != oldValue { onState?(state) } }
    }

    /// Synchronous critical section. Never awaits while held, so it is safe
    /// to call from async contexts (unlike raw lock()/unlock(), which the
    /// compiler flags there).
    private func locked<R>(_ body: () -> R) -> R {
        lock.lock()
        defer { lock.unlock() }
        return body()
    }

    // MARK: - Lifecycle

    func connect() {
        let alreadyRunning = locked {
            let was = shouldRun
            shouldRun = true
            return was
        }
        guard !alreadyRunning else { return }
        Task { await openSocket() }
    }

    func disconnect() {
        locked {
            shouldRun = false
            topics.removeAll()
            joinRefs.removeAll()
            pendingReplies.removeAll()
        }
        teardown()
        state = .disconnected
    }

    private func openSocket() async {
        // Explicit self: the local `task` below otherwise wins the capture
        // and Xcode 26.6 rejects it as used before declaration.
        let proceed = locked { shouldRun && self.task == nil }
        guard proceed else { return }

        state = .connecting
        // Three plain statements on purpose: touching this closure property
        // inside a guard condition (binding it or awaiting it) crashes the
        // Xcode 26.6 type checker under StrictConcurrency (fixed in 27).
        let provider = urlProvider
        let resolved = await provider()
        guard let url = resolved else {
            scheduleReconnect()
            return
        }

        let config = URLSessionConfiguration.default
        let session = URLSession(configuration: config, delegate: self, delegateQueue: nil)
        let task = session.webSocketTask(with: url)
        task.maximumMessageSize = 1 << 22

        let adopted = locked {
            guard shouldRun, self.task == nil else { return false }
            self.session = session
            self.task = task
            return true
        }
        guard adopted else {
            session.invalidateAndCancel()
            return
        }

        task.resume()
        receiveLoop(on: task)
    }

    private func teardown() {
        let (task, session) = locked { () -> (URLSessionWebSocketTask?, URLSession?) in
            let pair = (self.task, self.session)
            self.task = nil
            self.session = nil
            heartbeatAwaitingRef = nil
            return pair
        }
        heartbeatTask?.cancel()
        heartbeatTask = nil
        task?.cancel(with: .goingAway, reason: nil)
        session?.invalidateAndCancel()
    }

    private func scheduleReconnect() {
        let attempt: Int? = locked {
            guard shouldRun else { return nil }
            let current = reconnectAttempt
            reconnectAttempt += 1
            return current
        }
        guard let attempt else { return }

        teardown()
        state = .disconnected

        let base = Self.backoffMs[min(attempt, Self.backoffMs.count - 1)]
        let jitter = base * 0.25 * (Double.random(in: -1 ... 1))
        let delay = (base + jitter) / 1000
        log.info("realtime reconnect in \(delay, format: .fixed(precision: 2))s")
        Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
            await self?.openSocket()
        }
    }

    private func forceReconnect() {
        log.warning("realtime heartbeat watchdog fired; recycling socket")
        scheduleReconnect()
    }

    /// Fast-path recovery after app foregrounding: sockets die during
    /// suspension, and the watchdog/backoff can take ~30s to notice on their
    /// own. Verifies a live socket with an immediate heartbeat, or reconnects
    /// right away instead of waiting out the scheduled backoff.
    func nudge() {
        let (running, connected) = locked { (shouldRun, state == .connected && task != nil) }
        guard running else { return }
        if connected {
            let ref = nextRef()
            locked { heartbeatAwaitingRef = ref }
            sendFrame(topic: "phoenix", event: "heartbeat", payload: [:], ref: ref, joinRef: nil)
            Task { [weak self] in
                try? await Task.sleep(nanoseconds: 8_000_000_000)
                guard let self else { return }
                let stillWaiting = self.locked { self.heartbeatAwaitingRef == ref }
                if stillWaiting { self.forceReconnect() }
            }
        } else {
            locked { reconnectAttempt = 0 }
            Task { await openSocket() }
        }
    }

    // MARK: - URLSessionWebSocketDelegate

    func urlSession(
        _ session: URLSession,
        webSocketTask: URLSessionWebSocketTask,
        didOpenWithProtocol protocol: String?
    ) {
        let joined: Set<String>? = locked {
            guard webSocketTask === task else { return nil }
            reconnectAttempt = 0
            return topics
        }
        guard let joined else { return }
        state = .connected
        for topic in joined { sendJoin(topic) }
        startHeartbeat()
    }

    func urlSession(
        _ session: URLSession,
        webSocketTask: URLSessionWebSocketTask,
        didCloseWith closeCode: URLSessionWebSocketTask.CloseCode,
        reason: Data?
    ) {
        let isCurrent = locked { webSocketTask === task }
        if isCurrent { scheduleReconnect() }
    }

    // MARK: - Channels

    func join(_ topic: String) {
        let connected = locked {
            topics.insert(topic)
            return state == .connected && task != nil
        }
        if connected { sendJoin(topic) }
    }

    func leave(_ topic: String) {
        let (joinRef, connected) = locked { () -> (String?, Bool) in
            topics.remove(topic)
            return (joinRefs.removeValue(forKey: topic), task != nil)
        }
        guard connected else { return }
        sendFrame(topic: topic, event: "phx_leave", payload: [:], ref: nextRef(), joinRef: joinRef)
    }

    func push(
        topic: String,
        event: String,
        payload: [String: Any],
        completion: ((Bool, [String: Any]) -> Void)? = nil
    ) {
        let ref = nextRef()
        let joinRef = locked { () -> String? in
            if let completion { pendingReplies[ref] = completion }
            return joinRefs[topic]
        }
        sendFrame(topic: topic, event: event, payload: payload, ref: ref, joinRef: joinRef)
    }

    private func sendJoin(_ topic: String) {
        let ref = nextRef()
        let payload = joinPayloadProvider?(topic) ?? [:]
        locked {
            joinRefs[topic] = ref
            pendingReplies[ref] = { [weak self] ok, response in
                self?.onJoin?(topic, ok, response)
            }
        }
        sendFrame(topic: topic, event: "phx_join", payload: payload, ref: ref, joinRef: ref)
    }

    // MARK: - Heartbeat

    private func startHeartbeat() {
        heartbeatTask?.cancel()
        heartbeatTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 25_000_000_000)
                guard let self, !Task.isCancelled else { return }
                let ref = self.nextRef()
                self.locked { self.heartbeatAwaitingRef = ref }
                self.sendFrame(topic: "phoenix", event: "heartbeat", payload: [:], ref: ref, joinRef: nil)
                try? await Task.sleep(nanoseconds: 8_000_000_000)
                if Task.isCancelled { return }
                let stillWaiting = self.locked { self.heartbeatAwaitingRef == ref }
                if stillWaiting {
                    self.forceReconnect()
                    return
                }
            }
        }
    }

    // MARK: - Wire

    private func nextRef() -> String {
        locked {
            refCounter += 1
            return String(refCounter)
        }
    }

    private func sendFrame(topic: String, event: String, payload: [String: Any], ref: String?, joinRef: String?) {
        var frame: [String: Any] = ["topic": topic, "event": event, "payload": payload]
        if let ref { frame["ref"] = ref }
        if let joinRef { frame["join_ref"] = joinRef }
        guard JSONSerialization.isValidJSONObject(frame),
              let data = try? JSONSerialization.data(withJSONObject: frame)
        else {
            log.error("realtime: failed to encode \(topic)/\(event)")
            return
        }
        let task = locked { self.task }
        task?.send(.string(String(decoding: data, as: UTF8.self))) { [weak self] error in
            if error != nil { self?.scheduleReconnect() }
        }
    }

    private func receiveLoop(on task: URLSessionWebSocketTask) {
        task.receive { [weak self] result in
            guard let self else { return }
            let isCurrent = self.locked { self.task === task }
            guard isCurrent else { return }

            switch result {
            case .failure:
                self.scheduleReconnect()
            case let .success(message):
                switch message {
                case let .string(text):
                    self.handleFrame(Data(text.utf8))
                case let .data(data):
                    self.handleFrame(data)
                @unknown default:
                    break
                }
                self.receiveLoop(on: task)
            }
        }
    }

    private func handleFrame(_ data: Data) {
        guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
        let message = PhoenixMessage(
            topic: object["topic"] as? String ?? "",
            event: object["event"] as? String ?? "",
            payload: object["payload"] as? [String: Any] ?? [:],
            ref: object["ref"] as? String
        )

        switch message.event {
        case "phx_reply":
            guard let ref = message.ref else { return }
            let ok = (message.payload["status"] as? String) == "ok"
            let response = message.payload["response"] as? [String: Any] ?? [:]
            let completion = locked { () -> ((Bool, [String: Any]) -> Void)? in
                if heartbeatAwaitingRef == ref { heartbeatAwaitingRef = nil }
                return pendingReplies.removeValue(forKey: ref)
            }
            completion?(ok, response)
        case "phx_error":
            // Channel crashed server-side; rejoin shortly if still registered.
            let registered = locked { topics.contains(message.topic) }
            if registered {
                let topic = message.topic
                Task { [weak self] in
                    try? await Task.sleep(nanoseconds: 1_000_000_000)
                    guard let self else { return }
                    let still = self.locked { self.topics.contains(topic) && self.state == .connected }
                    if still { self.sendJoin(topic) }
                }
            }
        case "phx_close":
            break
        default:
            onEvent?(message)
        }
    }
}
