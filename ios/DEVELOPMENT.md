# Warmbly iOS — local development guide

This is the end-to-end guide for running the iOS app against a local backend. If you already have the native dev stack running, skip to [First run](#first-run).

## Prerequisites

- Xcode 26 or newer. The project targets iOS 26 and uses the folder-synchronized project format, so any `.swift` file added under `ios/Warmbly/` is compiled automatically. You never edit the `.pbxproj` to add files.
- The repo's dev stack running natively (below). The Go services and frontends run on the host; only the backing infrastructure runs in Docker.

## Step 1: start the backend stack

From the repo root, in separate terminals (leave them running):

```
make infra        # postgres, redis, kafka, mailpit, localstack, etc. in Docker. Run once.
make backend      # the API on :8080 (applies migrations on boot)
make realtime     # the Phoenix websocket service on :4000 (live updates + presence)
make seed         # one-shot: loads fixtures and dev users. Safe to re-run.
```

`make realtime` is optional but recommended: without it the app still works, but lists won't update live and presence is empty. It needs the Elixir toolchain on the host. `make run` is a shortcut that runs backend + consumer + worker in one terminal if you'd rather not manage three.

Local service map:

| Service | URL | Notes |
| --- | --- | --- |
| Backend API | `http://localhost:8080` | the app calls `<origin>/v1` |
| Realtime (Phoenix) | `ws://localhost:4000/socket/websocket` | fetched automatically via `POST /v1/getaway` |
| Mailpit (dev email) | `http://localhost:18025` | where login OTP codes land (offset from the usual 8025) |

## Step 2: run the app

1. Open `ios/Warmbly.xcodeproj` in Xcode.
2. Pick the **Warmbly** scheme and an iOS 26 simulator.
3. Run (Cmd-R).

On a simulator the defaults already point at `http://localhost:8080`, and `Info.plist` allows local (non-HTTPS) networking via `NSAllowsLocalNetworking`, so no ATS changes are needed.

## First run

The app opens on an animated welcome screen; **Get started** begins account creation, **I already have an account** goes to sign-in. The gear button (top-right on the welcome screen, and on the email step's card) opens **Connection settings**:

- **Server origin** — bare origin, no path. Default `http://localhost:8080`. The app appends `/v1` itself.
- **Turnstile token** — pre-filled with the dev bypass token `warmbly-local-turnstile-bypass`. The dev backend accepts it so you don't need a real captcha widget. (Production is captcha-gated and needs a real token.)

Then sign in with a seeded account:

| Email | Password | Workspace flavor |
| --- | --- | --- |
| `dev@warmbly.com` | `password123` | baseline (has mailboxes, campaigns, warmup data) |
| `alex@acme.test` | `password123` | free tier |
| `beth@beta.test` | `password123` | pro |
| `gus@gamma.test` | `password123` | dedicated worker |

Password login is two-step by design:

1. Enter email + password → the backend emails a **6-digit code**.
2. Open Mailpit at `http://localhost:18025`, find the newest message, and enter the code.

After login the app resolves your workspace (auto-selects if you have exactly one, otherwise shows a picker), then lands on the tab bar. You can also register a brand-new account from the app; registration is the same two-step OTP flow, and the confirmation code is in Mailpit too. A brand-new account (password or social) goes through a short onboarding questionnaire (name, how you found us, optional role and team size) before entering the app.

### Social sign-in (Apple / Google)

The email step shows native **Sign in with Apple** and **Continue with Google** buttons based on what the connected backend advertises at `GET /v1/auth/providers`:

- **Apple** — enabled whenever the backend has an expected bundle ID (`APPLE_IOS_BUNDLE_ID`, default `com.warmbly.app`). The app sends the Apple identity token to `POST /v1/auth/apple`, which verifies it against Apple's public keys. Trying it end-to-end needs a signed build and a simulator/device signed into an Apple ID.
- **Google** — hidden until the backend sets `GOOGLE_IOS_CLIENT_ID` (an iOS OAuth client ID from Google Cloud Console). The app runs the OAuth ceremony in an in-app browser with PKCE, exchanges the code on device (iOS clients have no secret), and sends the ID token to `POST /v1/auth/google`.

First-time social sign-ins auto-provision the user, workspace, and trial server-side; there is no OTP step because the provider identity is already verified.

## Pointing at another host (staging, self-hosted)

The server is an in-app runtime setting, not a build-time constant, so one binary serves everyone: the hosted SaaS, a self-hosted instance, or a local stack. Users switch hosts from the login screen's Connection settings; nobody rebuilds the app to reach their own infrastructure.

- Shipped (Release) builds default the origin to `https://api.warmbly.com`, so ordinary customers just log in and never open the setting.
- Debug builds default to `http://localhost:8080` for the native dev stack.
- Enter any other origin (a staging box, a self-hosted instance) in Connection settings; it persists in `UserDefaults`. "Reset to default" returns to the build's default.
- App Transport Security: a remote/custom host must be `https://`. Plain `http://` is allowed only to localhost and LAN/reserved addresses (dev and on-device testing). See the [README caveat](README.md).

## Running on a physical device

1. On the connection screen, set the server origin to your Mac's LAN address, e.g. `http://192.168.1.20:8080` (find it with `ipconfig getifaddr en0`).
2. Make sure `make backend` is reachable on the LAN. It binds `0.0.0.0:8080` by default, so it usually is; check any host firewall.
3. Realtime is auto-derived from the backend's `WEBSOCKET_URL`, which points at `:4000`. If you run on a device, start the backend with `PUBLIC_HOST=<your-lan-ip>` (or the equivalent) so the socket URL the backend hands back is reachable from the phone, not `localhost`.

## Verifying realtime works

Live updates and presence are a core behavior, so it's worth confirming:

- The **More** tab shows a connection status row: emerald pulsing "Live" when the socket is connected, amber "Connecting", red "Offline".
- Open the same workspace in the web dashboard (`make web`) and in the app. Editing a contact or starting a campaign in one should refresh the other's list within a second, and you should see each other's presence avatars on shared records.
- If it stays amber/red: confirm `make realtime` is running and its `/health` responds (`curl http://localhost:4000/health`), and that the backend's `WEBSOCKET_URL` resolves from the device/simulator.

## Making changes to the app

Because there is no iOS SDK on CI-less machines, the fast local signal for Swift changes is a syntax parse (it won't typecheck against UIKit/SwiftUI, but it catches structural errors):

```
cd ios && find Warmbly -name '*.swift' -print0 | xargs -0 swiftc -parse
```

A real typecheck happens when you build in Xcode. Conventions to keep the code consistent:

- New Codable models declare explicit `CodingKeys` (the decoder does not convert snake_case) and copy the Go json tags exactly. Prefer optional fields so a missing key never fails a whole screen.
- Reach the app graph with `@Environment(AppEnvironment.self) private var env` → `env.api` (the `/v1` client), `env.session`, `env.realtime`, `env.badges`.
- Every list: load with `.task`, reload on the matching `env.realtime.pulse(for:)`, and support `.refreshable`. Gate mutating actions with `env.session.can(.somePermission)`.
- Copy is sentence case, terse, and avoids em dashes.

## Troubleshooting

| Symptom | Likely cause / fix |
| --- | --- |
| "Couldn't reach the server" | `make backend` not running, or wrong origin. Test `curl http://localhost:8080/health`. On a device, use the LAN IP, not `localhost`. |
| Login says the code is invalid/expired | The email session is 10 minutes and allows 3 attempts. Use the newest Mailpit message; start over if it expired. |
| No OTP email in Mailpit | Confirm `make infra` is up and the backend is sending to Mailpit's SMTP (`:11025`). Check the backend log for send errors. |
| Stuck on the workspace picker | Your account has 0 or several orgs. Create one or pick one; a fresh login always starts with no org selected and the app switches you in. |
| Lists never update live | Realtime not connected. See [Verifying realtime works](#verifying-realtime-works). |
| A screen is empty for a teammate account | Permission-gated. Some data (e.g. campaign analytics) is owner-only server-side; the app surfaces this rather than erroring. |
| Captcha error on login | The bypass token was cleared. Reset it to `warmbly-local-turnstile-bypass` in Connection settings. |

## What stays on the web

By design, a few flows link out instead of being reimplemented on mobile: mailbox OAuth connect (Gmail/Outlook), campaign sequence editing, and plan changes/billing management. SMTP/IMAP mailbox connect does work natively. See the [README](README.md#known-limits) for the full list.
