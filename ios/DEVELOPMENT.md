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

Every build starts on Warmbly Cloud, so open the gear on the sign-in screen once and pick **Local development**; the choice persists. `Info.plist` allows local (non-HTTPS) networking via `NSAllowsLocalNetworking`, so no ATS changes are needed.

## First run

The app opens on an animated welcome screen; **Get started** begins account creation, **I already have an account** goes to sign-in. The gear button (top-right on sign-in) opens the **Server** sheet, which offers three choices:

- **Warmbly Cloud**: the default, `https://api.warmbly.com`.
- **Self-hosted**: any origin you type, bare with no path. The app appends `/v1` itself and adds `https://` when the scheme is missing (`http://` for localhost and LAN addresses).
- **Local development** (debug builds): `http://localhost:8080`, with the dev bypass token `warmbly-local-turnstile-bypass`, because the simulator can't use App Attest.

Under the choices, the sheet reads the server's public `GET /v1/auth/config` and reports whether it answers. A self-hosted server that asks for a captcha but takes no App Attest proof (`app_attest` false) is marked "Email sign-in off": the app never renders Turnstile, so email and password sign-in there needs `APP_ATTEST_APP_IDS` on the server, or a dev backend's bypass token under **Advanced**. Apple, Google and passkey sign-in work regardless.

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
- **Google** — with `GOOGLE_IOS_CLIENT_ID` set (an iOS OAuth client ID from Google Cloud Console), the app runs the OAuth ceremony in an in-app browser with PKCE, exchanges the code on device (iOS clients have no secret), and sends the ID token to `POST /v1/auth/google`. Without it, when `GET /v1/auth/config` lists `google` and its `app_url` is a host the app is associated with (`AppConfig.associatedWebHosts`, today `app.warmbly.com`), the app runs the website's own Google sign-in instead: `POST /v1/auth/google/begin`, the returned URL in an in-app browser, the backend callback's redirect to `https://app.warmbly.com/auth/sso?code=` caught by the browser session, and `POST /v1/auth/sso/exchange` with the code and the binding from begin. Otherwise the button is hidden.

First-time social sign-ins auto-provision the user, workspace, and trial server-side; there is no OTP step because the provider identity is already verified. An Apple or Google sign-in on an address that already has a password asks for that password once (`POST /v1/auth/sso/link`) before the identity is linked.

### Saved passwords

The entitlement `webcredentials:app.warmbly.com` and the dashboard's `/.well-known/apple-app-site-association` (in `web/public`) let iOS AutoFill offer passwords saved for the dashboard on the email and password fields, and save new ones there. The App ID needs the Associated Domains capability; automatic signing with `-allowProvisioningUpdates` enables it. A domain change takes effect after Apple's CDN refetches the file, so a fresh install picks it up first. Self-hosted dashboards are not associated with the App Store build.

### Confirming a recent sign-in

Connecting a grant's mailboxes and deleting the account need a recent sign-in, asked for by the **Confirm it's you** sheet (`Features/Auth/ReauthSheet.swift`). It reads `GET /v1/auth/reauth/methods` and offers only what the account can use: **Continue with Apple** or **Continue with Google** for a linked identity the server can verify (`native_providers`), then the password, and a two-factor code when 2FA is on. A provider confirmation calls `POST /v1/auth/reauth/native/begin` for a single-use nonce, runs the provider sheet with it, and sends the ID token to `POST /v1/auth/reauth/native`; password and code go to `POST /v1/auth/reauth`. An account with none of these is told to sign out and back in. Passkeys and browser-only providers confirm on the web, not in the app. If the methods call fails, the sheet falls back to the password and code form.

## Pointing at another host (staging, self-hosted)

The server is an in-app runtime setting, not a build-time constant, so one binary serves everyone: the hosted SaaS, a self-hosted instance, or a local stack. Users switch hosts from the Server sheet on the sign-in screen; nobody rebuilds the app to reach their own infrastructure.

- Every build defaults the origin to Warmbly Cloud (`https://api.warmbly.com`), so ordinary customers just log in and never open the setting.
- Pick **Local development** for the native dev stack, or **Self-hosted** for any other origin (a staging box, a self-hosted instance); the choice persists in `UserDefaults`. Picking Warmbly Cloud again clears the bypass token and realtime override.
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

A real typecheck happens when you build in Xcode.

## Layout on iPad, iPhone Duo and landscape

One layout adapts to the window rather than to the device (`DesignSystem/AdaptiveLayout.swift`), so Split View, Stage Manager, rotation and folding all land on the same rules:

- **Phone-width windows** (under 600pt: a phone in portrait, the iPhone Duo folded, a narrow iPad split) keep the floating tab bar at the bottom.
- **Wide windows** (iPad, the iPhone Duo unfolded, a phone in landscape) move the same bar to a rail on the leading edge (`WTabRail`), where a thumb rests when holding a tablet or an unfolded phone, and leave the height to content. Command-1 to Command-5 switch tabs and Command-N runs the round action on a hardware keyboard.
- **Inbox, Campaigns and Contacts** (`ListDetailShell`) show the open item beside the list once there is 760pt of content, and pin the drawer open as a first column past 1180pt. Without a pinned drawer the list ends at the middle of the window, so on the unfolded iPhone Duo the list and the open conversation each sit on their own half of the fold. Whatever is open moves between the stack and the detail column when the window changes size; the move rebuilds it, so a page pushed inside it and unsaved edits there start over, while an instant reply keeps its undo.
- **Pages** (settings, analytics, CRM lists, mailboxes) keep their content at a reading width (`readableMargins`), and Home becomes a two-column dashboard past 840pt.
- The iPhone Duo cover screen puts the status bar in a strip down the trailing edge. Backgrounds run under it and content stays clear of it, through the safe area.

To see the unfolded Duo without folding a simulator, launch on an iPad with `WARMBLY_DEBUG_FRAME=951x669` (below).

### Scripted simulator runs

Debug builds read launch environment variables so a script can open the app signed in on a given tab without tapping through sign-in (both are compiled out of Release builds):

- `WARMBLY_DEBUG_TOKEN`: the JSON body returned by `POST /v1/auth/login/confirm`. It is written to the Keychain on launch, and the push permission prompt is skipped.
- `WARMBLY_DEBUG_TAB`: `home`, `inbox`, `campaigns`, `contacts` or `more`.
- `WARMBLY_DEBUG_HOME`: the Home hero style to render, `immersive` (the default), `card` or `fade`.
- `WARMBLY_DEBUG_SCROLL`: opens Home, the settings hub, a settings page or Analytics scrolled to the `center` or `bottom`, for screenshots below the hero.
- `WARMBLY_DEBUG_THREAD`: with `WARMBLY_DEBUG_TAB=inbox`, `1` opens the newest conversation once the list loads `end` also scrolls it to the bottom, where an inbox agent's drafted reply sits, and `edit` opens that draft in the reply composer.
- `WARMBLY_DEBUG_DRAWER`: `1` opens the drawer on launch (Inbox, Campaigns or Contacts, whichever tab is open, or Deals, Tasks and Meetings when one is opened from settings).
- `WARMBLY_DEBUG_CAMPAIGN`: with `WARMBLY_DEBUG_TAB=campaigns`, opens the campaign whose name contains the value (the first campaign when nothing matches).
- `WARMBLY_DEBUG_CONTACT`: with `WARMBLY_DEBUG_TAB=contacts`, opens the contact whose name or address contains the value. `WARMBLY_DEBUG_SORT` sets the contacts sort by its API name (`first_name`, `created_at`, ...).
- `WARMBLY_DEBUG_SETTINGS`: with `WARMBLY_DEBUG_TAB=more`, opens a settings page (`profile`, `notifications`, `security`, `billing`, `team`, `templates` or `activity`) or a full-screen browser (`analytics`, `mailboxes`, `deals`, `tasks` or `meetings`). `WARMBLY_DEBUG_NOTIFICATIONS=preferences` opens the notifications page on its preferences. `WARMBLY_DEBUG_ANALYTICS` picks the analytics page (`overview`, `deliverability`, `warmup` or `accounts`), and `WARMBLY_DEBUG_ANALYTICS_ACCOUNT=1` then opens a mailbox's health. `WARMBLY_DEBUG_TEMPLATE=1` opens the first template and `WARMBLY_DEBUG_AUDIT_ENTRY=1` the newest activity entry. With `mailboxes`, `WARMBLY_DEBUG_CONNECT` opens the add-a-mailbox flow on a step (`provider`, `google`, `twostep`, `apppassword`, `appconnect`, `account`, `imap`, `smtp`, `grants`, `users`, `vendors`, `vendorkey`, `vendormailboxes`, `file`, `filereview` or `running`); every step past `provider` uses sample data, including an admin grant, a vendor catalog and a file preview, and `home` opens the first step with that data.
- `WARMBLY_DEBUG_PHASE`: opens a screen from before the app, leaving the stored session alone: `launching` holds the splash, `signedout` shows sign-in, `onboarding` the first-run questions and `workspace` the create-a-workspace step. With `signedout`, `WARMBLY_DEBUG_AUTH` opens a later sign-in step (`signup`, `password`, `verify`, `twofa`, `newpassword` or `reset`), or `server` to open the Server sheet; with `onboarding`, `WARMBLY_DEBUG_ONBOARDING` opens `referral`, `role` or `team`.
- `WARMBLY_DEBUG_ASSISTANT`: opens Remie on launch. `1` opens it empty, any other text is sent as the first message, and `demo` or `demo-approval` plays a scripted run (tool steps, a streamed answer and, for the second, an approval card) without calling the AI provider, which the local stack does not configure.
- `WARMBLY_DEBUG_FRAME`: renders the app in a window of `<width>x<height>` points, scaled to fit, so an iPad simulator can show another device's layout. `951x669` is an unfolded iPhone Duo, `1376x1032` a 13-inch iPad in landscape. It sets the size only: safe-area insets and full-screen covers still come from the host, so check those on the target simulator.

`simctl` forwards variables prefixed with `SIMCTL_CHILD_`:

```
SIMCTL_CHILD_WARMBLY_DEBUG_TOKEN="$(cat token.json)" SIMCTL_CHILD_WARMBLY_DEBUG_TAB=inbox \
  xcrun simctl launch booted com.warmbly.app
xcrun simctl io booted screenshot inbox.png
``` Conventions to keep the code consistent:

- New Codable models declare explicit `CodingKeys` (the decoder does not convert snake_case) and copy the Go json tags exactly. Prefer optional fields so a missing key never fails a whole screen.
- Reach the app graph with `@Environment(AppEnvironment.self) private var env` → `env.api` (the `/v1` client), `env.session`, `env.realtime`, `env.badges`.
- Every list: load with `.task`, reload on the matching `env.realtime.pulse(for:)`, and support `.refreshable`. Gate mutating actions with `env.session.can(.somePermission)`.
- Copy is sentence case, terse, and avoids em dashes.
- Style with the `WTheme` tokens and `DesignSystem/` primitives, never raw `.white`/`.black` or system grouped backgrounds: `WTheme.canvas`/`raised`/`fill` surfaces, `hairline`/`hairlineStrong` borders, `ink`/`inkSecondary`/`inkTertiary` text, `InkButtonStyle` for the primary action, `WFont.meta` for meta (`WFont.code` only for real code), sentence case everywhere (no uppercased labels). List tabs use `GlassHeaderBar` and drawers `DrawerWorkspaceHeader`. Settings pages sit on `WTheme.groupedPage` with `groupedCard` cards (`SettingsPage`, `SettingsSection`, and `coverCard` from `SettingsCoverKit.swift`); a list that needs swipe actions is an `.insetGrouped` `List` with `CoverSectionHeader` headers instead. Sky (`WTheme.accent`) is for data and live state, not chrome.

## Troubleshooting

| Symptom | Likely cause / fix |
| --- | --- |
| "Couldn't reach the server" | `make backend` not running, or wrong origin. Test `curl http://localhost:8080/health`. On a device, use the LAN IP, not `localhost`. |
| Login says the code is invalid/expired | The email session is 10 minutes and allows 3 attempts. Use the newest Mailpit message; start over if it expired. |
| No OTP email in Mailpit | Confirm `make infra` is up and the backend is sending to Mailpit's SMTP (`:11025`). Check the backend log for send errors. |
| Stuck on the workspace picker | Your account has 0 or several orgs. Create one or pick one; a fresh login always starts with no org selected and the app switches you in. |
| Lists never update live | Realtime not connected. See [Verifying realtime works](#verifying-realtime-works). |
| A screen is empty for a teammate account | Permission-gated. Some data (e.g. campaign analytics) is owner-only server-side; the app surfaces this rather than erroring. |
| Captcha error on login | The server verifies a captcha and got neither an App Attest proof nor a bypass token. In the simulator against a dev backend, set its `TURNSTILE_BYPASS_TOKEN` under Advanced; on a device, the server needs `APP_ATTEST_APP_IDS` naming the app (and `APP_ATTEST_ALLOW_DEVELOPMENT=true` for an Xcode build); a self-host can also run with `CAPTCHA_PROVIDER=none`. |
| "Couldn't verify this copy of the app" | The server refused the App Attest proof (`app_attest_failed`). A development-signed build needs `APP_ATTEST_ALLOW_DEVELOPMENT=true`, and `APP_ATTEST_APP_IDS` must name `<team id>.com.warmbly.app` for the team that signed it. |

## Releasing to TestFlight and the App Store

The app ships for iPhone and iPad (`TARGETED_DEVICE_FAMILY = "1,2"`). Every merge to `main` that touches `ios/` runs `.github/workflows/ios-release.yml` on a `macos-26` runner: it archives a Release build, signs it with cloud-managed certificates through an App Store Connect API key, and uploads it to TestFlight. The build number is the workflow's run number; the version is `MARKETING_VERSION` in the project. **Run workflow** on the Actions tab uploads a build without a merge. Promoting a build to the App Store stays a manual step in App Store Connect.

### One-time setup

In the Apple Developer portal and App Store Connect:

1. Under **Certificates, Identifiers & Profiles > Identifiers**, register the App ID `com.warmbly.app` with Push Notifications, Sign in with Apple and App Attest.
2. In App Store Connect, create the app (iOS, bundle id `com.warmbly.app`, SKU `warmbly-ios`).
3. Under **Users and Access > Integrations > App Store Connect API**, create a team key with the **Admin** role (cloud-managed signing creates certificates, which needs Admin). Download the `.p8` once and note the key id and the issuer id.
4. On the app's **TestFlight** tab, create an internal group and add your testers; new builds reach them automatically.

In GitHub, create an environment named `app-store` (**Settings > Environments**), limit its deployment branches to `main`, and add:

```sh
gh variable set APPLE_TEAM_ID --env app-store --body "<team id>"
gh secret set ASC_KEY_ID --env app-store --body "<key id>"
gh secret set ASC_ISSUER_ID --env app-store --body "<issuer id>"
base64 -i AuthKey_<key id>.p8 | gh secret set ASC_KEY_P8 --env app-store
```

Environment secrets never reach a pull request from a fork, and the job only runs in `warmbly/warmbly`. Delete the `.p8` from your machine once it is stored.

### Before the first submission

- **Review account.** Create an account for App Review on the production instance, then turn on **Skip emailed sign-in code** for it in the admin panel (or `warmblyctl user login-code-exempt`), so a reviewer can sign in with only the password. Give them a workspace with sample mailboxes and campaigns, and put the credentials in the review notes.
- **App Attest.** The production backend must accept the App Store build: `APP_ATTEST_APP_IDS` defaults to `APPLE_TEAM_ID.APPLE_IOS_BUNDLE_ID`, so setting `APPLE_TEAM_ID` is enough.
- **Store listing.** iPhone screenshots (6.9-inch) and, because the app runs natively on iPad, iPad screenshots (13-inch), the privacy policy URL `https://warmbly.com/privacy/`, a support URL, and the App Privacy answers, which match `Warmbly/PrivacyInfo.xcprivacy`: email address, name, emails and other user content, linked to the user, used for app functionality, no tracking.
- **Billing.** The app shows the plan, usage and credits and sells nothing; it carries no purchase link, because plans are bought outside the App Store.

### Shipping a version

Bump `MARKETING_VERSION` in the project (Xcode: target > General > Version) in the change that starts the new version. Each merge after that is another build of the same version in TestFlight. When one is ready, attach it to a new version in App Store Connect and submit it for review.

A fork that wants its own build changes `PRODUCT_BUNDLE_IDENTIFIER`, registers that App ID, and sets up its own `app-store` environment; the job's repository check needs the fork's name too.

## What stays on the web

By design, a few flows stay on the dashboard instead of being reimplemented on mobile: setting up a new admin grant, campaign sequence editing, and changing a plan or payment method (the app shows billing read-only). Mailbox connect works natively with app passwords, existing admin grants (connecting a grant's mailboxes asks for a recent sign-in, see [Confirming a recent sign-in](#confirming-a-recent-sign-in)), inbox vendor keys, file and pasted imports (`POST /emails/imports/preview`, then `POST /emails/imports`) and Warmbly Cloud workspace mailboxes; per-mailbox Google or Microsoft sign-in is not offered. See the [README](README.md#known-limits) for the full list.
