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
        #if DEBUG
        .modifier(DebugWindowFrame())
        #endif
        .task {
            if env.session.phase == .launching {
                await env.session.bootstrap()
            }
        }
    }
}

/// Splash shown while the persisted session is validated: the breathing mark
/// on the theme's canvas, continuing the system launch color in either theme.
struct LaunchView: View {
    @Environment(\.colorScheme) private var scheme
    @State private var breathe = false

    var body: some View {
        ZStack {
            if scheme == .dark {
                SkyBackdrop()
            } else {
                Color("LaunchBackground").ignoresSafeArea()
            }
            WarmblyLogo()
                .fill(scheme == .dark ? Color.white : WTheme.ink)
                .frame(width: 56, height: 57)
                .shadow(
                    color: (scheme == .dark ? Color.white : WTheme.accent).opacity(breathe ? 0.4 : 0.08),
                    radius: breathe ? 28 : 10
                )
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
                    .fill(WTheme.onSky)
                    .frame(width: 56, height: 57)
                Text("Can't reach Warmbly")
                    .font(.body.weight(.semibold))
                    .foregroundStyle(WTheme.onSky)
                Text("You're still signed in. Retrying automatically.")
                    .font(.footnote)
                    .foregroundStyle(WTheme.onSky.opacity(0.85))
                Button {
                    Task { await env.session.bootstrap() }
                } label: {
                    Text("Try again")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(WTheme.onSky)
                        .padding(.horizontal, 18)
                        .padding(.vertical, 9)
                        .glassEffect(.regular.interactive(), in: Capsule())
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
