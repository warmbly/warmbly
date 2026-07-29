import SwiftUI

struct RootView: View {
    @Environment(AppEnvironment.self) private var env

    var body: some View {
        Group {
            switch env.session.phase {
            case .launching:
                LaunchView()
            case .loggedOut:
                AuthFlowView()
            case .unreachable:
                UnreachableView()
            case .onboarding:
                OnboardingFlowView()
            case .selectOrg:
                OrgPickerView()
            case .ready:
                MainTabView()
            }
        }
        .animation(.easeInOut(duration: 0.2), value: env.session.phase)
        .task {
            if env.session.phase == .launching {
                await env.session.bootstrap()
            }
        }
    }
}

/// Splash: the brand's airy sky with the mark, shown while the persisted
/// session is validated. The mark breathes instead of showing a spinner;
/// matches the auth backdrop so the handoff is seamless.
struct LaunchView: View {
    @State private var breathe = false

    var body: some View {
        ZStack {
            SkyBackdrop()
            WarmblyLogo()
                .fill(.white)
                .frame(width: 56, height: 57)
                .shadow(color: .white.opacity(breathe ? 0.5 : 0.12), radius: breathe ? 28 : 10)
                .scaleEffect(breathe ? 1.06 : 0.96)
                .animation(.easeInOut(duration: 1.1).repeatForever(autoreverses: true), value: breathe)
                .onAppear { breathe = true }
        }
    }
}

/// The backend is down or unreachable but a session is stored: keep the
/// login, say so, and retry on a timer instead of dropping to sign-in.
struct UnreachableView: View {
    @Environment(AppEnvironment.self) private var env

    var body: some View {
        ZStack {
            SkyBackdrop()
            VStack(spacing: 14) {
                WarmblyLogo()
                    .fill(.white)
                    .frame(width: 56, height: 57)
                Text("Can't reach Warmbly")
                    .font(.body.weight(.semibold))
                    .foregroundStyle(.white)
                Text("You're still signed in. Retrying automatically.")
                    .font(.footnote)
                    .foregroundStyle(.white.opacity(0.85))
                Button {
                    Task { await env.session.bootstrap() }
                } label: {
                    Text("Try again")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.white)
                        .padding(.horizontal, 18)
                        .padding(.vertical, 9)
                        .background(.white.opacity(0.18), in: Capsule())
                }
                .buttonStyle(TapScaleStyle())
            }
        }
        .task {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(4))
                guard !Task.isCancelled, env.session.phase == .unreachable else { break }
                await env.session.bootstrap()
            }
        }
    }
}
