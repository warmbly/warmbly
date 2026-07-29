import Foundation

/// Async HTTP client for the Warmbly `/v1` API.
///
/// Owns the token pair: attaches the access token, refreshes it behind a
/// single-flight lock (refresh tokens are strictly single-use server-side),
/// retries exactly once on 401, and signals auth failure so the app can
/// return to the login screen.
actor APIClient {
    enum Method: String {
        case get = "GET", post = "POST", patch = "PATCH", put = "PUT", delete = "DELETE"
    }

    private var token: AuthToken?
    private var refreshTask: Task<AuthToken, Error>?
    private let session: URLSession
    private var authFailureHandler: (@Sendable () -> Void)?

    private static let tokenKey = "auth_token"

    init() {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 30
        session = URLSession(configuration: config)
        if let raw = KeychainStore.get(Self.tokenKey),
           let stored = try? Self.decoder.decode(AuthToken.self, from: Data(raw.utf8)) {
            token = stored
        }
    }

    // MARK: - Coders

    static let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        // ISO8601FormatStyle is Sendable, unlike ISO8601DateFormatter.
        let fractional = Date.ISO8601FormatStyle(includingFractionalSeconds: true)
        let plain = Date.ISO8601FormatStyle()
        decoder.dateDecodingStrategy = .custom { d in
            let container = try d.singleValueContainer()
            let raw = try container.decode(String.self)
            if let date = (try? fractional.parse(raw)) ?? (try? plain.parse(raw)) { return date }
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Unparseable date: \(raw)")
        }
        return decoder
    }()

    static let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }()

    // MARK: - Token management

    func onAuthFailure(_ handler: @escaping @Sendable () -> Void) {
        authFailureHandler = handler
    }

    var hasToken: Bool { token != nil }

    func setToken(_ newToken: AuthToken?) {
        token = newToken
        if let newToken, let data = try? Self.encoder.encode(newToken) {
            KeychainStore.set(String(decoding: data, as: UTF8.self), for: Self.tokenKey)
        } else {
            KeychainStore.remove(Self.tokenKey)
        }
    }

    private func validAccessToken() async throws -> String {
        if let refreshTask { return try await refreshTask.value.accessToken }
        guard let token else {
            // No token at all: kick straight to sign-in instead of stranding
            // the screen on an inline "session expired" error.
            failAuth()
            throw APIError.unauthorized(nil)
        }
        // Proactive refresh shortly before expiry, mirroring the web client.
        if token.accessTokenExpiresAt.timeIntervalSinceNow > 30 { return token.accessToken }
        return try await refresh().accessToken
    }

    @discardableResult
    private func refresh() async throws -> AuthToken {
        if let refreshTask { return try await refreshTask.value }
        guard let current = token else {
            failAuth()
            throw APIError.unauthorized(nil)
        }
        // Don't burn the single-use refresh nonce on a doomed call.
        guard current.refreshTokenExpiresAt.timeIntervalSinceNow > 0 else {
            failAuth()
            throw APIError.unauthorized(nil)
        }
        let task = Task<AuthToken, Error> { [session] in
            let url = try Self.baseURL().appending(path: "auth/refresh")
            var request = URLRequest(url: url)
            request.httpMethod = "POST"
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try Self.encoder.encode(["refresh_token": current.refreshToken])
            let data: Data
            let response: URLResponse
            do {
                (data, response) = try await session.data(for: request)
            } catch {
                // Offline / server down is NOT a dead session.
                throw APIError.network(error)
            }
            guard let http = response as? HTTPURLResponse else { throw APIError.network(URLError(.badServerResponse)) }
            guard http.statusCode == 200 else {
                let payload = try? Self.decoder.decode(APIErrorPayload.self, from: data)
                throw APIError.unauthorized(payload)
            }
            return try Self.decoder.decode(AuthToken.self, from: data)
        }
        refreshTask = task
        do {
            let fresh = try await task.value
            refreshTask = nil
            setToken(fresh)
            return fresh
        } catch {
            refreshTask = nil
            // Only a definitive rejection kills the session; a network failure
            // keeps the tokens so the user stays signed in through blips.
            if case APIError.network = error {
                throw error
            }
            failAuth()
            throw error
        }
    }

    private func failAuth() {
        setToken(nil)
        authFailureHandler?()
    }

    // MARK: - Request building

    private static func baseURL() throws -> URL {
        guard let url = AppConfig.apiBaseURL else { throw APIError.notConfigured }
        return url
    }

    private func buildRequest(
        _ method: Method,
        _ path: String,
        query: [String: String?],
        body: Data?,
        idempotencyKey: String?
    ) throws -> URLRequest {
        var url = try Self.baseURL()
        for part in path.split(separator: "/") { url.append(path: String(part)) }
        if !query.isEmpty {
            let items = query.compactMap { key, value in value.map { URLQueryItem(name: key, value: $0) } }
            if !items.isEmpty { url.append(queryItems: items) }
        }
        var request = URLRequest(url: url)
        request.httpMethod = method.rawValue
        request.setValue(UUID().uuidString, forHTTPHeaderField: "X-Request-Id")
        if let body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        if let idempotencyKey {
            request.setValue(idempotencyKey, forHTTPHeaderField: "Idempotency-Key")
        }
        return request
    }

    private func execute(_ request: URLRequest, authorized: Bool) async throws -> (Data, HTTPURLResponse) {
        var request = request
        if authorized {
            let access = try await validAccessToken()
            request.setValue("Bearer \(access)", forHTTPHeaderField: "Authorization")
        }
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw APIError.network(error)
        }
        guard let http = response as? HTTPURLResponse else {
            throw APIError.network(URLError(.badServerResponse))
        }

        if http.statusCode == 401, authorized {
            // Refresh once and retry; a second 401 means the session is dead.
            _ = try await refresh()
            var retry = request
            retry.setValue("Bearer \(try await validAccessToken())", forHTTPHeaderField: "Authorization")
            let (retryData, retryResponse) = try await session.data(for: retry)
            guard let retryHTTP = retryResponse as? HTTPURLResponse else {
                throw APIError.network(URLError(.badServerResponse))
            }
            if retryHTTP.statusCode == 401 {
                failAuth()
                throw APIError.unauthorized(try? Self.decoder.decode(APIErrorPayload.self, from: retryData))
            }
            return (retryData, retryHTTP)
        }
        return (data, http)
    }

    private func validate(_ data: Data, _ http: HTTPURLResponse) throws {
        switch http.statusCode {
        case 200 ..< 300:
            return
        case 401:
            throw APIError.unauthorized(try? Self.decoder.decode(APIErrorPayload.self, from: data))
        case 429:
            let payload = try? Self.decoder.decode(APIErrorPayload.self, from: data)
            throw APIError.rateLimited(retryAfterMs: payload?.retryAfterMs)
        default:
            throw APIError.server(
                status: http.statusCode,
                payload: try? Self.decoder.decode(APIErrorPayload.self, from: data)
            )
        }
    }

    // MARK: - Public surface

    func request<T: Decodable>(
        _ method: Method,
        _ path: String,
        query: [String: String?] = [:],
        bodyData: Data? = nil,
        authorized: Bool = true,
        idempotencyKey: String? = nil
    ) async throws -> T {
        let request = try buildRequest(method, path, query: query, body: bodyData, idempotencyKey: idempotencyKey)
        let (data, http) = try await execute(request, authorized: authorized)
        try validate(data, http)
        if T.self == EmptyBody.self, data.isEmpty { return EmptyBody() as! T }
        do {
            return try Self.decoder.decode(T.self, from: data)
        } catch {
            throw APIError.decoding(error)
        }
    }

    func get<T: Decodable>(_ path: String, query: [String: String?] = [:], authorized: Bool = true) async throws -> T {
        try await request(.get, path, query: query, authorized: authorized)
    }

    func post<T: Decodable, B: Encodable>(
        _ path: String,
        body: B,
        query: [String: String?] = [:],
        authorized: Bool = true,
        idempotent: Bool = false
    ) async throws -> T {
        try await request(
            .post, path,
            query: query,
            bodyData: try Self.encoder.encode(body),
            authorized: authorized,
            idempotencyKey: idempotent ? UUID().uuidString : nil
        )
    }

    func post<T: Decodable>(_ path: String, query: [String: String?] = [:], authorized: Bool = true) async throws -> T {
        try await request(.post, path, query: query, authorized: authorized)
    }

    func patch<T: Decodable, B: Encodable>(_ path: String, body: B, idempotent: Bool = false) async throws -> T {
        try await request(
            .patch, path,
            bodyData: try Self.encoder.encode(body),
            idempotencyKey: idempotent ? UUID().uuidString : nil
        )
    }

    func put<T: Decodable, B: Encodable>(_ path: String, body: B) async throws -> T {
        try await request(.put, path, bodyData: try Self.encoder.encode(body))
    }

    func delete<T: Decodable>(_ path: String, query: [String: String?] = [:]) async throws -> T {
        try await request(.delete, path, query: query)
    }

    func delete<T: Decodable, B: Encodable>(_ path: String, body: B) async throws -> T {
        try await request(.delete, path, bodyData: try Self.encoder.encode(body))
    }

    // MARK: - Server-sent events

    /// POSTs and streams the response as SSE, yielding each `data:` payload.
    /// Auth mirrors `execute` (token attached, one refresh + retry on 401);
    /// cancelling the consuming task aborts the request, which cancels the
    /// run server-side. The long timeout covers slow multi-tool agent runs.
    func stream<B: Encodable>(_ path: String, body: B) async throws -> AsyncThrowingStream<Data, Error> {
        var request = try buildRequest(.post, path, query: [:], body: try Self.encoder.encode(body), idempotencyKey: nil)
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        request.timeoutInterval = 600

        var (bytes, http) = try await openStream(request)
        if http.statusCode == 401 {
            _ = try await refresh()
            (bytes, http) = try await openStream(request)
            if http.statusCode == 401 {
                failAuth()
                throw APIError.unauthorized(nil)
            }
        }
        guard (200 ..< 300).contains(http.statusCode) else {
            // Error responses are plain JSON; drain a bounded body for the payload.
            var data = Data()
            for try await byte in bytes {
                data.append(byte)
                if data.count > 64 * 1024 { break }
            }
            try validate(data, http)
            throw APIError.network(URLError(.badServerResponse))
        }

        return AsyncThrowingStream { continuation in
            let reader = Task {
                do {
                    for try await line in bytes.lines {
                        guard line.hasPrefix("data:") else { continue }
                        let payload = line.dropFirst(5).trimmingCharacters(in: .whitespaces)
                        guard !payload.isEmpty else { continue }
                        continuation.yield(Data(payload.utf8))
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: APIError.network(error))
                }
            }
            continuation.onTermination = { _ in reader.cancel() }
        }
    }

    private func openStream(_ request: URLRequest) async throws -> (URLSession.AsyncBytes, HTTPURLResponse) {
        var request = request
        let access = try await validAccessToken()
        request.setValue("Bearer \(access)", forHTTPHeaderField: "Authorization")
        let bytes: URLSession.AsyncBytes
        let response: URLResponse
        do {
            (bytes, response) = try await session.bytes(for: request)
        } catch {
            throw APIError.network(error)
        }
        guard let http = response as? HTTPURLResponse else {
            throw APIError.network(URLError(.badServerResponse))
        }
        return (bytes, http)
    }
}
