import AuthenticationServices
import CryptoKit
import SwiftUI

/// Native Sign in with Apple plus Google via an in-app web ceremony: the iOS
/// client when the backend advertises one, else the website's own Google
/// sign-in handing back to the app. Rendered optimistically: until the
/// providers answer, both buttons show (the hosted service supports both);
/// once a backend has neither, its button disappears. One shipped binary,
/// every deployment.
struct SocialSignInRow: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.colorScheme) private var colorScheme

    let providers: AuthProvidersInfo?
    @Binding var busy: Bool
    let onError: (String) -> Void
    /// A sign-in that ends in a 2FA or link challenge rather than a session.
    var onChallenge: (SocialOutcome) -> Void = { _ in }

    @State private var googleFlow = GoogleSignInFlow()

    private var showApple: Bool { providers == nil || providers?.appleEnabled == true }
    private var showGoogle: Bool { providers == nil || providers?.googleAvailable == true }

    var body: some View {
        VStack(spacing: 12) {
            if showApple {
                SignInWithAppleButton(.continue) { request in
                    request.requestedScopes = [.email, .fullName]
                } onCompletion: { result in
                    handleApple(result)
                }
                .signInWithAppleButtonStyle(colorScheme == .dark ? .white : .black)
                // The UIKit button keeps the style it was built with; rebuild it when the theme lands.
                .id(colorScheme)
                .frame(height: 54)
                .clipShape(Capsule())
                .disabled(busy)
            }

            if showGoogle {
                Button {
                    signInWithGoogle()
                } label: {
                    HStack(spacing: 10) {
                        GoogleGlyph()
                            .frame(width: 19, height: 19)
                        Text("Continue with Google")
                            .font(.system(size: 16.5, weight: .medium))
                    }
                    .frame(maxWidth: .infinity)
                    .frame(height: 54)
                    .background(WTheme.raised, in: Capsule())
                    .overlay(Capsule().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
                }
                .buttonStyle(PressableButtonStyle())
                .disabled(busy)
            }
        }
    }

    private func handleApple(_ result: Result<ASAuthorization, Error>) {
        switch result {
        case let .success(authorization):
            guard
                let credential = authorization.credential as? ASAuthorizationAppleIDCredential,
                let tokenData = credential.identityToken,
                let identityToken = String(data: tokenData, encoding: .utf8)
            else {
                onError("Apple didn't return a usable credential. Please try again.")
                return
            }
            let first = credential.fullName?.givenName
            let last = credential.fullName?.familyName
            busy = true
            Task {
                defer { busy = false }
                do {
                    let outcome = try await env.session.signInWithApple(identityToken: identityToken, firstName: first, lastName: last)
                    if case .loggedIn = outcome { return }
                    onChallenge(outcome)
                } catch {
                    onError((error as? APIError)?.errorDescription ?? error.localizedDescription)
                }
            }
        case let .failure(error):
            // Dismissing the Apple sheet is not an error worth surfacing.
            if let authError = error as? ASAuthorizationError, authError.code == .canceled { return }
            onError(error.localizedDescription)
        }
    }

    private func signInWithGoogle() {
        guard let providers, providers.googleAvailable else {
            onError("Google sign-in isn't set up on this server yet.")
            return
        }
        busy = true
        Task {
            defer { busy = false }
            do {
                let outcome: SocialOutcome
                if let clientID = providers.googleClientID {
                    let idToken = try await googleFlow.signIn(clientID: clientID)
                    outcome = try await env.session.signInWithGoogle(idToken: idToken)
                } else if let host = providers.googleBrowserHost {
                    let begun = try await env.session.beginBrowserSignIn("google")
                    let code = try await googleFlow.handoffCode(from: begun.url, callbackHost: host)
                    outcome = try await env.session.finishBrowserSignIn(code: code, binding: begun.binding)
                } else {
                    return
                }
                if case .loggedIn = outcome { return }
                onChallenge(outcome)
            } catch GoogleSignInFlow.Failure.cancelled {
                // User closed the sheet; stay quiet.
            } catch {
                onError((error as? APIError)?.errorDescription ?? error.localizedDescription)
            }
        }
    }
}

/// The multicolor Google "G", drawn locally so no asset is needed. Segment
/// angles are lifted from the official 18x18 mark: the ring is open between
/// the red arm's diagonal cut (-50°) and the crossbar (12°).
struct GoogleGlyph: View {
    var body: some View {
        Canvas { context, size in
            let side = min(size.width, size.height)
            let center = CGPoint(x: size.width / 2, y: size.height / 2)
            let lineWidth = side * (3.58 / 18)
            let radius = side / 2 - lineWidth / 2

            func arc(_ from: Double, _ to: Double, _ hex: UInt32) {
                var path = Path()
                path.addArc(center: center, radius: radius, startAngle: .degrees(from), endAngle: .degrees(to), clockwise: false)
                context.stroke(path, with: .color(Color(hex: hex)), style: StrokeStyle(lineWidth: lineWidth))
            }

            arc(-161.3, -49.8, 0xEA4335) // red: top
            arc(161.3, 198.7, 0xFBBC05) // yellow: left
            arc(56.2, 161.3, 0x34A853) // green: bottom
            arc(11.8, 56.2, 0x4285F4) // blue: lower right

            // The crossbar of the G.
            let bar = CGRect(
                x: center.x,
                y: center.y - lineWidth / 2,
                width: side / 2 - side * (0.36 / 18),
                height: lineWidth
            )
            context.fill(Path(bar), with: .color(Color(hex: 0x4285F4)))
        }
    }
}

/// Google Sign-In without the SDK: an `ASWebAuthenticationSession` OAuth
/// ceremony with PKCE against the iOS client ID the backend advertises. iOS
/// OAuth clients are public (no secret), so the code exchange happens on
/// device and only the resulting ID token is sent to our backend.
@MainActor
final class GoogleSignInFlow: NSObject, ASWebAuthenticationPresentationContextProviding {
    enum Failure: Error {
        case cancelled
        case malformedResponse
    }

    private var activeSession: ASWebAuthenticationSession?
    private var presentationWindow: UIWindow?

    /// `nonce`, when set, is echoed in the ID token's `nonce` claim.
    func signIn(clientID: String, nonce: String? = nil) async throws -> String {
        // Captured up front (we're on the main actor) so the presentation
        // callback never has to conjure a window of its own.
        guard let window = Self.keyWindow else { throw Failure.malformedResponse }
        presentationWindow = window
        defer { presentationWindow = nil }

        let verifier = Self.randomURLSafe(bytes: 32)
        let challenge = Self.base64URL(SHA256.hash(data: Data(verifier.utf8)))
        // Google's iOS redirect scheme is the reversed client ID.
        let scheme = Self.reversedClientScheme(clientID)
        let redirectURI = "\(scheme):/oauth2redirect"

        var authorize = URLComponents(string: "https://accounts.google.com/o/oauth2/v2/auth")!
        authorize.queryItems = [
            URLQueryItem(name: "client_id", value: clientID),
            URLQueryItem(name: "redirect_uri", value: redirectURI),
            URLQueryItem(name: "response_type", value: "code"),
            URLQueryItem(name: "scope", value: "openid email profile"),
            URLQueryItem(name: "code_challenge", value: challenge),
            URLQueryItem(name: "code_challenge_method", value: "S256"),
        ]
        if let nonce {
            authorize.queryItems?.append(URLQueryItem(name: "nonce", value: nonce))
        }

        let callback = try await runWebAuth(url: authorize.url!, callback: .customScheme(scheme))
        guard
            let items = URLComponents(url: callback, resolvingAgainstBaseURL: false)?.queryItems,
            let code = items.first(where: { $0.name == "code" })?.value
        else {
            throw Failure.malformedResponse
        }

        return try await exchange(code: code, clientID: clientID, redirectURI: redirectURI, verifier: verifier)
    }

    /// The website's sign-in: the backend's callback redirects to
    /// `https://<callbackHost>/auth/sso?code=`, which the session catches
    /// because the app holds `webcredentials` for that host. The browser
    /// session shares Safari's cookies, so a Google account already signed in
    /// there is one tap.
    func handoffCode(from url: URL, callbackHost: String) async throws -> String {
        guard let window = Self.keyWindow else { throw Failure.malformedResponse }
        presentationWindow = window
        defer { presentationWindow = nil }

        let callback = try await runWebAuth(url: url, callback: .https(host: callbackHost, path: "/auth/sso"))
        guard
            let items = URLComponents(url: callback, resolvingAgainstBaseURL: false)?.queryItems,
            let code = items.first(where: { $0.name == "code" })?.value, !code.isEmpty
        else {
            throw Failure.malformedResponse
        }
        return code
    }

    private func runWebAuth(url: URL, callback: ASWebAuthenticationSession.Callback) async throws -> URL {
        try await withCheckedThrowingContinuation { continuation in
            let session = ASWebAuthenticationSession(url: url, callback: callback) { [weak self] callbackURL, error in
                Task { @MainActor in self?.activeSession = nil }
                if let error {
                    if let webError = error as? ASWebAuthenticationSessionError, webError.code == .canceledLogin {
                        continuation.resume(throwing: Failure.cancelled)
                    } else {
                        continuation.resume(throwing: error)
                    }
                    return
                }
                guard let callbackURL else {
                    continuation.resume(throwing: Failure.malformedResponse)
                    return
                }
                continuation.resume(returning: callbackURL)
            }
            session.presentationContextProvider = self
            activeSession = session
            if !session.start() {
                activeSession = nil
                continuation.resume(throwing: Failure.malformedResponse)
            }
        }
    }

    private func exchange(code: String, clientID: String, redirectURI: String, verifier: String) async throws -> String {
        var request = URLRequest(url: URL(string: "https://oauth2.googleapis.com/token")!)
        request.httpMethod = "POST"
        request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        let form = [
            "code": code,
            "client_id": clientID,
            "redirect_uri": redirectURI,
            "grant_type": "authorization_code",
            "code_verifier": verifier,
        ]
        request.httpBody = form
            .map { "\($0.key)=\($0.value.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? $0.value)" }
            .joined(separator: "&")
            .data(using: .utf8)

        let (data, response) = try await URLSession.shared.data(for: request)
        guard (response as? HTTPURLResponse)?.statusCode == 200 else { throw Failure.malformedResponse }

        struct TokenResponse: Decodable {
            let idToken: String
            enum CodingKeys: String, CodingKey { case idToken = "id_token" }
        }
        return try JSONDecoder().decode(TokenResponse.self, from: data).idToken
    }

    nonisolated func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        MainActor.assumeIsolated {
            // signIn guards that a window exists before the session starts.
            presentationWindow ?? Self.keyWindow!
        }
    }

    private static var keyWindow: UIWindow? {
        let windows = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
        return windows.first(where: \.isKeyWindow) ?? windows.first
    }

    // MARK: - Helpers

    /// "1234-abc.apps.googleusercontent.com" -> "com.googleusercontent.apps.1234-abc"
    static func reversedClientScheme(_ clientID: String) -> String {
        clientID.split(separator: ".").reversed().joined(separator: ".")
    }

    static func randomURLSafe(bytes count: Int) -> String {
        var bytes = [UInt8](repeating: 0, count: count)
        _ = SecRandomCopyBytes(kSecRandomDefault, count, &bytes)
        return base64URL(bytes)
    }

    static func base64URL(_ bytes: some Sequence<UInt8>) -> String {
        Data(bytes).base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}
