import SwiftUI

/// Interactive weekly sending-schedule board, reimagined for mobile: each day
/// is its own full-width horizontal 24h track (not a cramped column). Tap + to
/// add a window, drag a bar sideways to move it, drag its left/right handle to
/// resize, tap × to remove, long-press a day to copy it everywhere or clear it.
/// Everything snaps to 30 minutes. Writes the authoritative `schedule_windows`.
struct CampaignSchedulePage: View {
    @Environment(AppEnvironment.self) private var env

    let store: CampaignDetailStore

    @State private var windows: [[ScheduleInterval]] = Array(repeating: [], count: 7)
    @State private var baseline: [[ScheduleInterval]] = Array(repeating: [], count: 7)
    @State private var timezone: String = TimeZone.current.identifier
    @State private var baselineTZ: String = TimeZone.current.identifier
    @State private var seeded = false
    @State private var isSaving = false
    @State private var saveError: String?

    private var campaign: Campaign { store.campaign }
    private var canManage: Bool { env.session.can(.manageCampaigns) }

    private var totalWindows: Int { windows.reduce(0) { $0 + $1.count } }
    private var activeDays: Int { windows.filter { !$0.isEmpty }.count }
    private var dirty: Bool { windows != baseline || timezone != baselineTZ }

    var body: some View {
        ScrollView {
            VStack(spacing: 0) {
                summaryBar
                presetsBar
                CampaignScheduleBoard(windows: $windows, editable: canManage)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 14)
                    .background(
                        RoundedRectangle(cornerRadius: 22, style: .continuous)
                            .fill(Color(.secondarySystemGroupedBackground))
                    )
                    .overlay(
                        RoundedRectangle(cornerRadius: 22, style: .continuous)
                            .strokeBorder(Color(.separator).opacity(0.22), lineWidth: 1)
                    )
                    .padding(.horizontal, 12)
                    .padding(.top, 4)
                footer
            }
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(Color(.systemGroupedBackground))
        .navigationTitle("Schedule")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if canManage, dirty {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        Task { await save() }
                    } label: {
                        if isSaving { ProgressView().controlSize(.small) }
                        else { Text("Save").fontWeight(.semibold) }
                    }
                    .disabled(isSaving || totalWindows == 0)
                }
            }
        }
        .onAppear(perform: seedOnce)
        .alert("Couldn't save schedule", isPresented: Binding(
            get: { saveError != nil }, set: { if !$0 { saveError = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: { Text(saveError ?? "") }
    }

    // MARK: Header

    private var summaryBar: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 1) {
                EyebrowLabel("Weekly sending windows")
                if totalWindows == 0 {
                    Text("No windows yet — tap a day's track to add one.")
                        .font(.footnote)
                        .foregroundStyle(WTheme.negative)
                } else {
                    Text("\(totalWindows) window\(totalWindows == 1 ? "" : "s") across \(activeDays) day\(activeDays == 1 ? "" : "s")")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
            }
            Spacer(minLength: 8)
            Menu {
                Picker("Timezone", selection: $timezone) {
                    ForEach(commonTimezones, id: \.self) { tz in
                        Text(CampaignSchedulePage.cityName(tz)).tag(tz)
                    }
                }
            } label: {
                HStack(spacing: 4) {
                    Image(systemName: "globe")
                        .font(.system(size: 11, weight: .semibold))
                    Text(CampaignSchedulePage.cityName(timezone))
                        .font(.subheadline)
                        .lineLimit(1)
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.system(size: 10, weight: .semibold))
                }
                .foregroundStyle(WTheme.accent)
            }
            .disabled(!canManage)
        }
        .padding(.horizontal, 16)
        .padding(.top, 10)
        .padding(.bottom, 8)
    }

    /// The member's timezone folded in with a short common list so the picker is
    /// useful without a 400-row wall; the current value is always present.
    private var commonTimezones: [String] {
        var list = [
            "America/Los_Angeles", "America/Denver", "America/Chicago", "America/New_York",
            "America/Sao_Paulo", "Europe/London", "Europe/Berlin", "Europe/Athens",
            "Africa/Johannesburg", "Asia/Dubai", "Asia/Kolkata", "Asia/Singapore",
            "Asia/Tokyo", "Australia/Sydney", "Pacific/Auckland", TimeZone.current.identifier,
        ]
        if !list.contains(timezone) { list.append(timezone) }
        var seen = Set<String>()
        return list.filter { seen.insert($0).inserted }
    }

    private var presetsBar: some View {
        HStack(spacing: 8) {
            EyebrowLabel("Presets")
            presetChip("Weekdays 9–5") { weekdayWindows(start: 540, end: 1020) }
            presetChip("Every day 9–5") { Array(repeating: [ScheduleInterval(start: 540, end: 1020)], count: 7) }
            presetChip("Clear") { Array(repeating: [], count: 7) }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 6)
    }

    private func presetChip(_ title: String, build: @escaping () -> [[ScheduleInterval]]) -> some View {
        Button {
            withAnimation(.snappy) { windows = build() }
        } label: {
            Text(title)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(.secondary)
                .padding(.horizontal, 12)
                .frame(height: 30)
                .background(Color(.secondarySystemGroupedBackground), in: Capsule())
                .overlay(Capsule().strokeBorder(Color(.separator).opacity(0.4), lineWidth: 1))
        }
        .buttonStyle(TapScaleStyle())
        .disabled(!canManage)
    }

    private func weekdayWindows(start: Int, end: Int) -> [[ScheduleInterval]] {
        (0..<7).map { $0 < 5 ? [ScheduleInterval(start: start, end: end)] : [] }
    }

    private var footer: some View {
        Text("Each day is independent — tap an empty stretch to add a window there (several a day is fine, e.g. morning and afternoon), drag a bar to move it, tap it to set exact times. Drag two bars together and they merge into one. In \(CampaignSchedulePage.cityName(timezone)) time; run dates are set on the web.")
            .font(.caption)
            .foregroundStyle(.tertiary)
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: State

    private func seedOnce() {
        guard !seeded else { return }
        seeded = true
        let seed = campaign.seedDisplayWindows()
        windows = seed
        baseline = seed
        if let tz = campaign.timezone, !tz.isEmpty {
            timezone = tz
            baselineTZ = tz
        }
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        let wire = Campaign.wireWindows(fromDisplay: windows)
        let tz = timezone
        let previous = store.campaign
        await store.update(
            env.api,
            body: CampaignUpdateBody(timezone: tz, scheduleWindows: wire)
        ) {
            $0.timezone = tz
            $0.scheduleWindows = wire.map { Optional($0) }
        }
        if store.actionError != nil {
            saveError = store.actionError
            store.actionError = nil
        } else if store.campaign.updatedAt != previous.updatedAt || store.campaign.timezone == tz {
            baseline = windows
            baselineTZ = tz
        }
    }

    static func cityName(_ identifier: String) -> String {
        (identifier.split(separator: "/").last.map(String.init) ?? identifier)
            .replacingOccurrences(of: "_", with: " ")
    }
}

// MARK: - The board

private let kDay = 1440
private let kSnap = 30
private let kMinDur = 30

struct CampaignScheduleBoard: View {
    @Binding var windows: [[ScheduleInterval]]
    let editable: Bool

    private static let dayNames = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"]
    private static let dayShort = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
    private static let hourMarks = [0, 6, 12, 18, 24]
    private static let labelW: CGFloat = 46
    private static let rowH: CGFloat = 54
    private static let barH: CGFloat = 38
    private static let minBar: CGFloat = 38

    /// A softer, brighter sky than the deep accent — the bars used to read as
    /// dark slabs; sky-400 → sky-500 keeps white legible while feeling airy.
    private static let barGradient = LinearGradient(
        colors: [
            Color.dynamic(light: 0x38BDF8, dark: 0x0EA5E9),
            Color.dynamic(light: 0x0EA5E9, dark: 0x0369A1),
        ],
        startPoint: .top, endPoint: .bottom
    )

    private var todayIndex: Int { (Calendar.current.component(.weekday, from: Date()) + 5) % 7 }

    @State private var active: BlockDrag?
    @State private var editing: WindowRef?

    var body: some View {
        VStack(spacing: 8) {
            axisHeader
            VStack(spacing: 8) {
                ForEach(0..<7, id: \.self) { day in
                    dayRow(day)
                }
            }
        }
        .sheet(item: $editing) { ref in
            if ref.idx < windows[ref.day].count {
                ScheduleWindowEditor(
                    interval: Binding(
                        get: { windows[ref.day].indices.contains(ref.idx) ? windows[ref.day][ref.idx] : ScheduleInterval(start: 0, end: 60) },
                        set: { if windows[ref.day].indices.contains(ref.idx) { windows[ref.day][ref.idx] = $0 } }
                    ),
                    dayName: Self.dayNames[ref.day],
                    onDelete: {
                        removeWindow(ref.day, ref.idx)
                        editing = nil
                    },
                    onDone: {
                        withAnimation(.snappy) { windows[ref.day] = merge(windows[ref.day]) }
                        editing = nil
                    }
                )
                .presentationDetents([.height(320)])
                .presentationDragIndicator(.visible)
            }
        }
    }

    // MARK: Axis header (time labels across the track)

    private var axisHeader: some View {
        HStack(spacing: 8) {
            Color.clear.frame(width: Self.labelW)
            GeometryReader { geo in
                let W = geo.size.width
                ZStack(alignment: .topLeading) {
                    ForEach(Self.hourMarks, id: \.self) { h in
                        Text(hourLabel(h))
                            .font(.system(size: 9))
                            .monospacedDigit()
                            .foregroundStyle(.tertiary)
                            .fixedSize()
                            .frame(width: 24, alignment: alignFor(h))
                            .offset(x: clampF(x(h * 60, W) - 12, 0, max(0, W - 24)))
                    }
                }
            }
            .frame(height: 12)
        }
    }

    private func alignFor(_ h: Int) -> Alignment {
        if h == 0 { return .leading }
        if h == 24 { return .trailing }
        return .center
    }

    // MARK: One day = one full-width track

    private func dayRow(_ day: Int) -> some View {
        let on = !windows[day].isEmpty
        let isToday = day == todayIndex
        return HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 4) {
                    Text(Self.dayShort[day])
                        .font(.system(size: 13, weight: on ? .semibold : .medium))
                        .foregroundStyle(on ? AnyShapeStyle(Color.primary) : AnyShapeStyle(Color.secondary))
                    if isToday {
                        Circle().fill(WTheme.accent).frame(width: 4, height: 4)
                    }
                }
                if editable {
                    Button { addWindow(day) } label: {
                        Image(systemName: "plus")
                            .font(.system(size: 11, weight: .bold))
                            .foregroundStyle(WTheme.accent)
                            .frame(width: 26, height: 20)
                            .background(Tone.sky.background, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                    }
                    .buttonStyle(TapScaleStyle())
                }
            }
            .frame(width: Self.labelW, alignment: .leading)
            .contentShape(Rectangle())
            .contextMenu {
                if editable {
                    Button { copyToAll(day) } label: { Label("Copy \(Self.dayNames[day]) to all days", systemImage: "doc.on.doc") }
                    if on {
                        Button(role: .destructive) { clearDay(day) } label: { Label("Clear \(Self.dayNames[day])", systemImage: "xmark") }
                    }
                }
            }

            GeometryReader { geo in
                track(day: day, W: geo.size.width, isToday: isToday)
            }
            .frame(height: Self.rowH)
        }
        .frame(height: Self.rowH)
    }

    private func track(day: Int, W: CGFloat, isToday: Bool) -> some View {
        let on = !windows[day].isEmpty
        return ZStack(alignment: .leading) {
            // The empty track: tap anywhere blank to drop a window at that time.
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(isToday ? AnyShapeStyle(WTheme.accent.opacity(0.05)) : AnyShapeStyle(Color(.systemBackground).opacity(0.7)))
                .overlay(
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .strokeBorder(Color(.separator).opacity(0.3), lineWidth: 1)
                )
                .contentShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                .conditionalGesture(editable, SpatialTapGesture().onEnded { v in
                    addWindow(day, at: v.location.x, W: W)
                })
            // hour gridlines
            ForEach(Self.hourMarks.dropFirst().dropLast(), id: \.self) { h in
                Rectangle()
                    .fill(Color(.separator).opacity(0.4))
                    .frame(width: 0.5)
                    .frame(maxHeight: .infinity)
                    .offset(x: x(h * 60, W))
            }
            // A faint + in every open stretch, so it reads as "tap here to add".
            if editable {
                gapHints(day: day, W: W)
            }
            if !on, editable {
                HStack(spacing: 5) {
                    Image(systemName: "plus")
                        .font(.system(size: 11, weight: .bold))
                    Text("Add a sending window")
                        .font(.system(size: 11, weight: .medium))
                }
                .foregroundStyle(WTheme.accent.opacity(0.55))
                .frame(maxWidth: .infinity)
                .allowsHitTesting(false)
            }
            ForEach(Array(windows[day].enumerated()), id: \.offset) { idx, iv in
                bar(day: day, idx: idx, iv: iv, W: W)
            }
        }
        .frame(height: Self.rowH)
        .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
    }

    /// Faint + markers centred in each open gap between windows (and the ends),
    /// shown only when the day already has at least one window (the empty-day
    /// affordance is the centred "Add a sending window" hint instead).
    @ViewBuilder
    private func gapHints(day: Int, W: CGFloat) -> some View {
        if !windows[day].isEmpty {
            let centers = gapCenters(day: day, W: W)
            ForEach(Array(centers.enumerated()), id: \.offset) { _, cx in
                Image(systemName: "plus")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(WTheme.accent.opacity(0.32))
                    .frame(width: 16)
                    .offset(x: cx - 8)
            }
            .allowsHitTesting(false)
        }
    }

    /// Pixel x of the centre of every gap wide enough to hold a window.
    private func gapCenters(day: Int, W: CGFloat) -> [CGFloat] {
        let sorted = windows[day].sorted { $0.start < $1.start }
        var out: [CGFloat] = []
        var cursor = 0
        func consider(_ from: Int, _ to: Int) {
            let px = CGFloat(to - from) / CGFloat(kDay) * W
            guard to - from >= 120, px >= 44 else { return }
            out.append(x((from + to) / 2, W))
        }
        for iv in sorted {
            consider(cursor, iv.start)
            cursor = max(cursor, iv.end)
        }
        consider(cursor, kDay)
        return out
    }

    // MARK: A window bar

    private func bar(day: Int, idx: Int, iv: ScheduleInterval, W: CGFloat) -> some View {
        let left = x(iv.start, W)
        let raw = x(iv.end, W) - left
        let w = max(raw, Self.minBar)
        let wide = w >= 86
        return ZStack {
            RoundedRectangle(cornerRadius: 9, style: .continuous)
                .fill(Self.barGradient)
                .overlay(
                    RoundedRectangle(cornerRadius: 9, style: .continuous)
                        .strokeBorder(.white.opacity(0.18), lineWidth: 0.5)
                )
                .shadow(color: Color(hex: 0x0EA5E9).opacity(0.16), radius: 2.5, y: 1)

            HStack(spacing: 4) {
                if wide, editable {
                    Image(systemName: "line.3.horizontal")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(.white.opacity(0.65))
                }
                Text(wide ? "\(fmt(iv.start)) – \(fmt(iv.end))" : fmt(iv.start))
                    .font(.system(size: 10.5, weight: .semibold))
                    .monospacedDigit()
                    .foregroundStyle(.white)
                    .shadow(color: .black.opacity(0.22), radius: 0.5, y: 0.5)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
            }
            .padding(.horizontal, 7)
        }
        .frame(width: w, height: Self.barH)
        .contentShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
        .onTapGesture { if editable { editing = WindowRef(day: day, idx: idx) } }
        .conditionalGesture(editable, moveGesture(day: day, idx: idx, W: W))
        .offset(x: left)
    }

    // MARK: Gestures (horizontal)

    // Global coordinate space so translation stays stable while the bar
    // re-lays-out under the finger (local space would feed back and jitter).
    private func moveGesture(day: Int, idx: Int, W: CGFloat) -> some Gesture {
        DragGesture(minimumDistance: 6, coordinateSpace: .global)
            .onChanged { g in applyDrag(day: day, idx: idx, mode: .move, delta: g.translation.width, span: W) }
            .onEnded { _ in endDrag(day: day) }
    }

    private func applyDrag(day: Int, idx: Int, mode: BlockDrag.Mode, delta: CGFloat, span W: CGFloat) {
        guard W > 0, idx < windows[day].count else { return }
        if active == nil {
            let iv = windows[day][idx]
            active = BlockDrag(day: day, idx: idx, mode: mode, origStart: iv.start, origEnd: iv.end)
        }
        guard let a = active, a.day == day, a.idx == idx else { return }
        let deltaMin = Int((delta / W) * CGFloat(kDay))
        var start = a.origStart
        var end = a.origEnd
        switch mode {
        case .move:
            let dur = a.origEnd - a.origStart
            start = clampInt(snap(a.origStart + deltaMin), 0, kDay - dur)
            end = start + dur
        case .resizeStart:
            start = clampInt(snap(a.origStart + deltaMin), 0, a.origEnd - kMinDur)
            end = a.origEnd
        case .resizeEnd:
            start = a.origStart
            end = clampInt(snap(a.origEnd + deltaMin), a.origStart + kMinDur, kDay)
        }
        windows[day][idx] = ScheduleInterval(start: start, end: end)
    }

    private func endDrag(day: Int) {
        active = nil
        withAnimation(.snappy) { windows[day] = merge(windows[day]) }
    }

    // MARK: Mutations

    private func addWindow(_ day: Int) {
        let sorted = windows[day].sorted { $0.start < $1.start }
        guard !sorted.isEmpty else {
            withAnimation(.snappy) { windows[day] = [ScheduleInterval(start: 9 * 60, end: 17 * 60)] }
            return
        }
        guard let slot = freeSlot(in: sorted) else { return }
        withAnimation(.snappy) { windows[day] = merge(sorted + [slot]) }
    }

    /// Tap a blank stretch of a day's track to drop a ~2h window centred on the
    /// tapped time, clamped to sit inside that gap. Overlapping the edge of a
    /// neighbour just merges into it (see `merge`), which is the natural
    /// "actually make it one longer window" gesture.
    private func addWindow(_ day: Int, at px: CGFloat, W: CGFloat) {
        guard editable, W > 0 else { return }
        let tap = clampInt(snap(Int(px / W * CGFloat(kDay))), 0, kDay)
        let sorted = windows[day].sorted { $0.start < $1.start }
        // Bars capture their own taps; if a tap still lands inside one, ignore.
        if sorted.contains(where: { tap >= $0.start && tap < $0.end }) { return }
        var lo = 0, hi = kDay
        for iv in sorted {
            if iv.end <= tap { lo = max(lo, iv.end) }
            if iv.start >= tap { hi = min(hi, iv.start); break }
        }
        guard hi - lo >= kMinDur else { return }
        let dur = min(120, hi - lo)
        let start = clampInt(snap(tap - dur / 2), lo, hi - dur)
        let end = min(start + dur, hi)
        withAnimation(.snappy) { windows[day] = merge(windows[day] + [ScheduleInterval(start: start, end: end)]) }
    }

    /// A fresh ~2h window dropped into a free gap and kept an hour clear of its
    /// neighbours so it stays a SEPARATE window (a second tap + on a day that
    /// already sends adds a distinct second timeframe, e.g. morning + afternoon).
    /// nil when the day has no room left.
    private func freeSlot(in sorted: [ScheduleInterval]) -> ScheduleInterval? {
        let gap = 60
        let dur = 120
        if let last = sorted.last, kDay - last.end >= gap + kMinDur {
            let start = snap(last.end + gap)
            return ScheduleInterval(start: start, end: snap(min(start + dur, kDay)))
        }
        if let first = sorted.first, first.start >= gap + kMinDur {
            let end = snap(first.start - gap)
            return ScheduleInterval(start: snap(max(0, end - dur)), end: end)
        }
        var cursor = sorted.first?.end ?? 0
        for iv in sorted.dropFirst() {
            if iv.start - cursor >= 2 * gap + kMinDur {
                let start = snap(cursor + gap)
                return ScheduleInterval(start: start, end: snap(min(start + dur, iv.start - gap)))
            }
            cursor = max(cursor, iv.end)
        }
        return nil
    }

    private func removeWindow(_ day: Int, _ idx: Int) {
        guard idx < windows[day].count else { return }
        withAnimation(.snappy) { windows[day].remove(at: idx) }
    }

    private func clearDay(_ day: Int) {
        withAnimation(.snappy) { windows[day] = [] }
    }

    private func copyToAll(_ day: Int) {
        let src = windows[day]
        withAnimation(.snappy) { windows = Array(repeating: src, count: 7) }
    }

    // MARK: Geometry helpers

    private func x(_ minutes: Int, _ W: CGFloat) -> CGFloat {
        CGFloat(minutes) / CGFloat(kDay) * W
    }

    private func hourLabel(_ h: Int) -> String {
        let hh = h % 24
        let ampm = hh < 12 ? "a" : "p"
        let h12 = hh % 12 == 0 ? 12 : hh % 12
        return "\(h12)\(ampm)"
    }
}

private func clampF(_ n: CGFloat, _ lo: CGFloat, _ hi: CGFloat) -> CGFloat { max(lo, min(hi, n)) }

private struct WindowRef: Identifiable {
    let day: Int
    let idx: Int
    var id: String { "\(day)-\(idx)" }
}

/// Precise tap-to-edit sheet for one window: start/end wheel pickers (snapped
/// to 30 minutes) plus remove. This is how narrow windows are tuned exactly on
/// mobile, where dragging a ~25pt bar can't be pixel-accurate.
private struct ScheduleWindowEditor: View {
    @Binding var interval: ScheduleInterval
    let dayName: String
    let onDelete: () -> Void
    let onDone: () -> Void

    private static let midnight = Calendar.current.startOfDay(for: Date())

    private func date(_ minutes: Int) -> Date {
        Self.midnight.addingTimeInterval(TimeInterval(minutes * 60))
    }
    private func minutes(_ date: Date) -> Int {
        let c = Calendar.current.dateComponents([.hour, .minute], from: date)
        return (c.hour ?? 0) * 60 + (c.minute ?? 0)
    }

    private var startBinding: Binding<Date> {
        Binding(
            get: { date(interval.start) },
            set: { d in
                let m = snap(minutes(d))
                interval = ScheduleInterval(start: min(m, interval.end - kMinDur), end: interval.end)
            }
        )
    }
    private var endBinding: Binding<Date> {
        Binding(
            get: { date(interval.end % kDay) },
            set: { d in
                var m = snap(minutes(d))
                if m == 0 { m = kDay } // midnight as an end means end-of-day
                interval = ScheduleInterval(start: interval.start, end: max(m, interval.start + kMinDur))
            }
        )
    }

    private var durationLabel: String {
        let mins = interval.end - interval.start
        let h = mins / 60, m = mins % 60
        if m == 0 { return "\(h)h" }
        if h == 0 { return "\(m)m" }
        return "\(h)h \(m)m"
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    DatePicker("Starts", selection: startBinding, displayedComponents: .hourAndMinute)
                    DatePicker("Ends", selection: endBinding, displayedComponents: .hourAndMinute)
                } header: {
                    Text("Sending window · \(durationLabel)")
                } footer: {
                    Text("Sends spread evenly across this window. Add another window to this day for a second timeframe.")
                }
                Section {
                    Button(role: .destructive, action: onDelete) {
                        HStack { Spacer(); Text("Remove window"); Spacer() }
                    }
                }
            }
            .navigationTitle("\(dayName)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done", action: onDone).fontWeight(.semibold)
                }
            }
        }
    }
}

private struct BlockDrag {
    enum Mode { case move, resizeStart, resizeEnd }
    let day: Int
    let idx: Int
    let mode: Mode
    let origStart: Int
    let origEnd: Int
}

private extension View {
    @ViewBuilder
    func conditionalGesture<G: Gesture>(_ enabled: Bool, _ gesture: G) -> some View {
        if enabled { self.gesture(gesture) } else { self }
    }
}

// MARK: - Interval math

private func snap(_ n: Int) -> Int { Int((Double(n) / Double(kSnap)).rounded()) * kSnap }
private func clampInt(_ n: Int, _ lo: Int, _ hi: Int) -> Int { max(lo, min(hi, n)) }

private func fmt(_ min: Int) -> String {
    let h = min / 60
    let m = min % 60
    let ampm = h < 12 ? "a" : "p"
    let h12 = h % 12 == 0 ? 12 : h % 12
    return m == 0 ? "\(h12)\(ampm)" : "\(h12):\(String(format: "%02d", m))\(ampm)"
}

/// Sort and coalesce touching/overlapping windows within one day.
private func merge(_ ivs: [ScheduleInterval]) -> [ScheduleInterval] {
    let sorted = ivs.filter { $0.end > $0.start }.sorted { $0.start < $1.start }
    var out: [ScheduleInterval] = []
    for iv in sorted {
        if var last = out.last, iv.start <= last.end {
            last.end = max(last.end, iv.end)
            out[out.count - 1] = last
        } else {
            out.append(iv)
        }
    }
    return out
}
