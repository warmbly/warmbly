import SwiftUI

// Small CRM-scoped presentation helpers shared across the module's screens.

// MARK: - Colors and formatting

enum CRMHex {
    /// Parses a `#rrggbb` string (stage/task-type colors) into a Color.
    static func color(_ hex: String?) -> Color? {
        guard var raw = hex?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty else { return nil }
        if raw.hasPrefix("#") { raw.removeFirst() }
        guard raw.count == 6, let value = UInt32(raw, radix: 16) else { return nil }
        return Color(hex: value)
    }
}

enum CRMFormat {
    private static let currencySymbols: [String: String] = [
        "USD": "$", "EUR": "€", "GBP": "£", "JPY": "¥", "INR": "₹", "BRL": "R$",
        "AUD": "A$", "CAD": "C$", "NZD": "NZ$", "HUF": "Ft ", "CHF": "CHF ",
        "SEK": "kr ", "NOK": "kr ", "DKK": "kr ", "PLN": "zł ", "CZK": "Kč ",
    ]

    static func symbol(for code: String) -> String {
        currencySymbols[code.uppercased()] ?? "\(code.uppercased()) "
    }

    /// Full currency amount, e.g. "$1,250" / "$1,250.50".
    static func currency(_ value: Double, code: String?) -> String {
        let resolved = (code?.isEmpty == false) ? code!.uppercased() : "USD"
        let wholeNumber = value.truncatingRemainder(dividingBy: 1) == 0
        return value.formatted(
            .currency(code: resolved).precision(.fractionLength(wholeNumber ? 0 : 2))
        )
    }

    /// Compact currency for stat strips, e.g. "$12.3k".
    static func compactCurrency(_ value: Double, code: String?) -> String {
        let resolved = (code?.isEmpty == false) ? code!.uppercased() : "USD"
        if abs(value) >= 1000 {
            return symbol(for: resolved) + WFormat.compact(Int(value))
        }
        return currency(value, code: resolved)
    }

    /// Short due-date label: "today 14:30", "tomorrow", "Jun 12".
    static func dueLabel(_ date: Date) -> String {
        let calendar = Calendar.current
        if calendar.isDateInToday(date) {
            return "today " + date.formatted(date: .omitted, time: .shortened)
        }
        if calendar.isDateInTomorrow(date) { return "tomorrow" }
        if calendar.isDateInYesterday(date) { return "yesterday" }
        return date.formatted(.dateTime.day().month(.abbreviated))
    }

    /// Meeting time label with the time always included.
    static func meetingTime(_ date: Date) -> String {
        let calendar = Calendar.current
        if calendar.isDateInToday(date) {
            return "today " + date.formatted(date: .omitted, time: .shortened)
        }
        if calendar.isDateInTomorrow(date) {
            return "tomorrow " + date.formatted(date: .omitted, time: .shortened)
        }
        return date.formatted(.dateTime.day().month(.abbreviated).hour().minute())
    }
}

// MARK: - Tone mappings

extension CRMDeal {
    var statusTone: Tone {
        switch status {
        case "won": .emerald
        case "lost": .rose
        default: .sky
        }
    }
}

extension CRMTask {
    var priorityTone: Tone {
        switch priority {
        case "urgent": .rose
        case "high": .orange
        case "low": .slate
        default: .sky
        }
    }
}

extension CRMMeeting {
    var statusTone: Tone {
        switch status {
        case "booked": .emerald
        case "rescheduled": .amber
        case "canceled": .rose
        case "no_show": .rose
        case "completed": .slate
        default: .slate
        }
    }

    var statusLabel: String {
        switch status {
        case "no_show": "no show"
        default: status ?? "booked"
        }
    }
}

// MARK: - Browser chrome

/// Contacts-style search pill for the CRM browsers: a rounded 44pt field with
/// a leading symbol (a drawer hamburger when `onMenu` is provided), clear
/// button and the presence stack riding the trailing edge, plus optional
/// circular accessories (create/close buttons) beside it.
struct CRMSearchBar<Accessory: View>: View {
    @Binding var query: String
    let prompt: String
    private let onMenu: (() -> Void)?
    private let menuLabel: String
    private let accessory: Accessory
    @FocusState private var focused: Bool

    init(
        query: Binding<String>,
        prompt: String,
        onMenu: (() -> Void)? = nil,
        menuLabel: String = "Open menu",
        @ViewBuilder accessory: () -> Accessory
    ) {
        _query = query
        self.prompt = prompt
        self.onMenu = onMenu
        self.menuLabel = menuLabel
        self.accessory = accessory()
    }

    var body: some View {
        HStack(spacing: 10) {
            HStack(spacing: 6) {
                if let onMenu {
                    Button {
                        focused = false
                        onMenu()
                    } label: {
                        Image(systemName: "line.3.horizontal")
                            .font(.system(size: 17, weight: .medium))
                            .foregroundStyle(.primary)
                            .frame(width: 38, height: 38)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(menuLabel)
                } else {
                    Image(systemName: "magnifyingglass")
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(.secondary)
                        .frame(width: 38, height: 38)
                }

                TextField(prompt, text: $query)
                    .font(.subheadline)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.search)
                    .focused($focused)

                if !query.isEmpty {
                    Button {
                        query = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .font(.system(size: 16))
                            .foregroundStyle(.tertiary)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Clear search")
                }

                PresenceAvatars()
                    .padding(.trailing, 8)
            }
            .frame(height: 44)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 22, style: .continuous))

            accessory
        }
        .padding(.horizontal, 12)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }
}

extension CRMSearchBar where Accessory == EmptyView {
    init(query: Binding<String>, prompt: String, onMenu: (() -> Void)? = nil, menuLabel: String = "Open menu") {
        self.init(query: query, prompt: prompt, onMenu: onMenu, menuLabel: menuLabel) { EmptyView() }
    }
}

/// 44pt circular action button riding beside the search pill (create/close).
struct CRMCircleButton: View {
    let symbol: String
    let label: String
    var weight: Font.Weight = .medium
    var size: CGFloat = 17
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: size, weight: weight))
                .foregroundStyle(.primary)
                .frame(width: 44, height: 44)
                .background(Color(.secondarySystemBackground), in: Circle())
        }
        .buttonStyle(TapScaleStyle())
        .accessibilityLabel(label)
    }
}

// MARK: - Browser shell

/// Shared constants for the CRM full-screen browsers.
enum CRMBrowser {
    static let sidebarWidth: CGFloat = 300
    static let spring = Animation.spring(response: 0.34, dampingFraction: 0.86)
}

/// Full-screen drawer-browser scaffold copying the campaign leads browser: the
/// main pane scales back (anchor trailing) when the drawer opens, a dim layer
/// closes on tap, the drawer slides from the leading edge with drag-to-close,
/// and an edge swipe from x < 44 opens it. Presented inside the feature's own
/// NavigationStack with the system bar hidden.
struct CRMBrowserShell<Main: View, Drawer: View>: View {
    @Binding var sidebarOpen: Bool
    let main: () -> Main
    /// Built with the top safe-area inset so the sky hero can run under it.
    let drawer: (CGFloat) -> Drawer

    @State private var sidebarDrag: CGFloat = 0

    init(
        sidebarOpen: Binding<Bool>,
        @ViewBuilder main: @escaping () -> Main,
        @ViewBuilder drawer: @escaping (CGFloat) -> Drawer
    ) {
        _sidebarOpen = sidebarOpen
        self.main = main
        self.drawer = drawer
    }

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                main()
                    .scaleEffect(sidebarOpen ? 0.97 : 1, anchor: .trailing)
                    .simultaneousGesture(
                        DragGesture(minimumDistance: 25)
                            .onEnded { value in
                                if !sidebarOpen, value.startLocation.x < 44, value.translation.width > 70 {
                                    withAnimation(CRMBrowser.spring) { sidebarOpen = true }
                                }
                            }
                    )
                if sidebarOpen {
                    Color.black.opacity(0.32)
                        .ignoresSafeArea()
                        .transition(.opacity)
                        .onTapGesture { close() }
                }
                drawer(geo.safeAreaInsets.top)
                    .frame(width: CRMBrowser.sidebarWidth)
                    .frame(maxHeight: .infinity)
                    .background(Color(.systemBackground))
                    .clipShape(UnevenRoundedRectangle(bottomTrailingRadius: 26, topTrailingRadius: 26, style: .continuous))
                    .shadow(color: .black.opacity(sidebarOpen ? 0.22 : 0), radius: 30, x: 6, y: 0)
                    .ignoresSafeArea()
                    .offset(x: (sidebarOpen ? 0 : -CRMBrowser.sidebarWidth - 40) + sidebarDrag)
                    .gesture(
                        DragGesture()
                            .onChanged { value in sidebarDrag = min(0, value.translation.width) }
                            .onEnded { value in
                                if value.translation.width < -80 || value.predictedEndTranslation.width < -160 {
                                    close()
                                } else {
                                    withAnimation(.spring(response: 0.32, dampingFraction: 0.86)) { sidebarDrag = 0 }
                                }
                            }
                    )
            }
        }
        .sensoryFeedback(.impact(weight: .light), trigger: sidebarOpen)
    }

    private func close() {
        withAnimation(CRMBrowser.spring) {
            sidebarOpen = false
            sidebarDrag = 0
        }
    }
}

// MARK: - Drawer chrome

/// Drawer layout shared by the CRM browsers, mirroring the contacts/leads
/// sidebars: a slim sky hero (logo + heavy title + optional subtitle + badge
/// strip) over a rounded white sheet that scrolls the scope rows.
struct CRMDrawer<Badges: View, Rows: View>: View {
    let title: String
    var subtitle: String? = nil
    let topInset: CGFloat
    @ViewBuilder let badges: () -> Badges
    @ViewBuilder let rows: () -> Rows

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            hero
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    rows()
                }
                .padding(.horizontal, 12)
                .padding(.top, 8)
                .padding(.bottom, 40)
            }
            .background {
                UnevenRoundedRectangle(topLeadingRadius: 24, topTrailingRadius: 24, style: .continuous)
                    .fill(Color(.systemBackground))
                    .shadow(color: .black.opacity(0.1), radius: 14, y: -4)
            }
        }
        .background(alignment: .top) {
            AirSkyWash().frame(height: 360)
        }
        .background(Color(.systemBackground))
    }

    private var hero: some View {
        VStack(alignment: .leading, spacing: 13) {
            HStack(spacing: 8) {
                WarmblyLogo()
                    .fill(.white)
                    .frame(width: 21, height: 21 * (764 / 746))
                Text(title)
                    .font(.system(size: 17.5, weight: .heavy))
                    .tracking(-0.4)
                    .foregroundStyle(.white)
            }
            if let subtitle, !subtitle.isEmpty {
                Text(subtitle)
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(.white.opacity(0.82))
                    .lineLimit(1)
            }
            HStack(spacing: 6) {
                badges()
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, topInset + 12)
        .padding(.bottom, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Translucent count badge on the drawer hero.
struct CRMDrawerBadge: View {
    let symbol: String
    let text: String
    var live = false
    var pingColor: Color = .white

    var body: some View {
        HStack(spacing: 5) {
            Image(systemName: symbol)
                .font(.system(size: 10.5, weight: .semibold))
                .foregroundStyle(.white.opacity(0.9))
                .modifier(PingEffect(active: live, color: pingColor))
            Text(text)
                .font(.footnote.weight(.medium))
                .monospacedDigit()
                .foregroundStyle(.white)
                .contentTransition(.numericText())
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 5.5)
        .background(.white.opacity(0.16), in: Capsule())
    }
}

/// Eyebrow section label between drawer row groups.
struct CRMDrawerSectionLabel: View {
    let text: String

    init(_ text: String) { self.text = text }

    var body: some View {
        EyebrowLabel(text)
            .padding(.horizontal, 14)
            .padding(.top, 14)
            .padding(.bottom, 6)
    }
}

/// Scope pill row in a CRM drawer: leading icon / dot / pulsing dot, title,
/// trailing live count, and the sliding sky capsule via matchedGeometryEffect.
/// Rows cascade in with a capped stagger when the drawer reveals.
struct CRMDrawerRow: View {
    var icon: String? = nil
    var dot: Color? = nil
    var pingDot: Color? = nil
    let title: String
    var count: Int? = nil
    let selected: Bool
    let index: Int
    let revealed: Bool
    let namespace: Namespace.ID
    /// Matched-geometry id; use one id per independent selection group.
    var matchedID = "crmdrawer-active"
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 13) {
                Group {
                    if let pingDot {
                        Circle()
                            .fill(pingDot)
                            .frame(width: 11, height: 11)
                            .modifier(PingEffect(active: true, color: pingDot))
                    } else if let dot {
                        Circle()
                            .fill(dot)
                            .frame(width: 11, height: 11)
                    } else if let icon {
                        Image(systemName: icon)
                            .font(.system(size: 15, weight: .medium))
                            .foregroundStyle(selected ? WTheme.accent : Color.secondary)
                    }
                }
                .frame(width: 24)
                Text(title)
                    .font(.subheadline.weight(selected ? .semibold : .medium))
                    .foregroundStyle(selected ? WTheme.accent : Color.primary)
                    .lineLimit(1)
                Spacer(minLength: 8)
                if let count, count > 0 {
                    Text(WFormat.compact(count))
                        .font(.footnote.weight(selected ? .semibold : .medium))
                        .monospacedDigit()
                        .foregroundStyle(selected ? WTheme.accent : Color.secondary)
                        .contentTransition(.numericText())
                }
            }
            .padding(.horizontal, 16)
            .frame(height: 44)
            .background {
                if selected {
                    Capsule()
                        .fill(Tone.sky.background)
                        .matchedGeometryEffect(id: matchedID, in: namespace)
                }
            }
            .contentShape(Capsule())
        }
        .buttonStyle(TapScaleStyle())
        .opacity(revealed ? 1 : 0)
        .offset(x: revealed ? 0 : -18)
        // Cap the stagger so long row lists don't cascade for seconds.
        .animation(
            .spring(response: 0.42, dampingFraction: 0.82)
                .delay(revealed ? 0.03 + min(Double(index), 14) * 0.024 : 0),
            value: revealed
        )
    }
}

/// Tracked-uppercase caption as a bare list row (never a sticky `Section`
/// header): an optional stage-colored dot or pulsing tone accent plus a live
/// count. Whitespace above is the group separator.
struct CRMListCaption: View {
    let title: String
    var count: Int? = nil
    var tone: Tone? = nil
    var dotColor: Color? = nil
    var ping = false

    var body: some View {
        HStack(spacing: 6) {
            if ping, let tone {
                CRMPingDot(color: tone.color)
                    .padding(.trailing, 2)
            } else if let dotColor {
                Circle()
                    .fill(dotColor)
                    .frame(width: 7, height: 7)
            }
            Text(title.uppercased())
                .font(.caption.weight(.semibold))
                .tracking(0.9)
                .foregroundStyle(tone?.color ?? Color.secondary)
            if let count {
                Text(WFormat.compact(count))
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
                    .contentTransition(.numericText())
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 20)
        .padding(.top, 18)
        .padding(.bottom, 6)
        .listRowInsets(EdgeInsets())
        .listRowSeparator(.hidden)
        .listRowBackground(Color(.systemBackground))
    }
}

/// End-of-list marker: the exact total for the current scope/search.
struct CRMEndMarker: View {
    let count: Int
    let singular: String
    let plural: String

    init(_ count: Int, _ singular: String, plural: String? = nil) {
        self.count = count
        self.singular = singular
        self.plural = plural ?? singular + "s"
    }

    var body: some View {
        HStack {
            Spacer()
            Text("\(count) \(count == 1 ? singular : plural)")
                .font(.footnote)
                .monospacedDigit()
                .foregroundStyle(.tertiary)
            Spacer()
        }
        .padding(.vertical, 10)
        .listRowSeparator(.hidden)
        .listRowBackground(Color(.systemBackground))
    }
}

// MARK: - Chips and accents

/// Colored capsule chip driven by a server hex color (stages, task types).
struct CRMColorChip: View {
    let text: String
    var hex: String?

    var body: some View {
        let color = CRMHex.color(hex) ?? WTheme.paused
        Text(text)
            .font(.system(size: 11, weight: .medium))
            .foregroundStyle(color)
            .padding(.horizontal, 8)
            .padding(.vertical, 3.5)
            .background(color.opacity(0.12), in: Capsule())
            .lineLimit(1)
    }
}

/// Small always-pulsing dot (overdue/red, today/sky accents).
struct CRMPingDot: View {
    var color: Color

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: 6, height: 6)
            .modifier(PingEffect(active: true, color: color))
    }
}
