import SwiftUI

/// Gmail-style schedule picker shared by the compose window and the reply
/// composer: one-tap presets first, then a calendar and a dedicated time row
/// for full control down to the minute. `onConfirm` runs when a preset is
/// tapped or Set is pressed, after `scheduledAt` is updated.
struct ComposeScheduleSheet: View {
    @Binding var isPresented: Bool
    @Binding var scheduledAt: Date
    let onConfirm: () -> Void

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(presets, id: \.label) { preset in
                        Button {
                            withAnimation(.snappy) { scheduledAt = preset.date }
                            onConfirm()
                            isPresented = false
                        } label: {
                            HStack(spacing: 12) {
                                Image(systemName: preset.symbol)
                                    .font(.system(size: 15, weight: .medium))
                                    .foregroundStyle(WTheme.accent)
                                    .frame(width: 26)
                                Text(preset.label)
                                    .font(.subheadline.weight(.medium))
                                    .foregroundStyle(.primary)
                                Spacer(minLength: 8)
                                Text(preset.date.formatted(date: .abbreviated, time: .shortened))
                                    .font(.footnote)
                                    .monospacedDigit()
                                    .foregroundStyle(.secondary)
                            }
                            .padding(.horizontal, 16)
                            .frame(minHeight: 46)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(TapScaleStyle())
                        Divider().padding(.leading, 54)
                    }

                    EyebrowLabel("Pick date and time")
                        .padding(.horizontal, 16)
                        .padding(.top, 20)
                        .padding(.bottom, 2)
                    DatePicker(
                        "Date",
                        selection: $scheduledAt,
                        in: Self.bounds,
                        displayedComponents: [.date]
                    )
                    .datePickerStyle(.graphical)
                    .padding(.horizontal, 12)
                    Divider()
                    HStack {
                        Text("Time")
                            .font(.subheadline.weight(.medium))
                        Spacer(minLength: 8)
                        DatePicker(
                            "Time",
                            selection: $scheduledAt,
                            in: Self.bounds,
                            displayedComponents: [.hourAndMinute]
                        )
                        .labelsHidden()
                    }
                    .padding(.horizontal, 16)
                    .frame(minHeight: 52)
                    Divider()
                    Text("Will send \(scheduledAt.formatted(date: .complete, time: .shortened))")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                }
                .padding(.bottom, 24)
            }
            .navigationTitle("Schedule send")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { isPresented = false }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Set") {
                        onConfirm()
                        isPresented = false
                    }
                    .fontWeight(.semibold)
                }
            }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
    }

    /// Scheduled sends must be > now+5s and <= now+29 days (server rule).
    static var bounds: ClosedRange<Date> {
        let lower = Date().addingTimeInterval(120)
        let upper = Date().addingTimeInterval(29 * 24 * 3600 - 3600)
        return lower ... upper
    }

    /// Gmail-style quick options; only ones inside the server bounds show.
    private var presets: [(label: String, symbol: String, date: Date)] {
        var presets: [(label: String, symbol: String, date: Date)] = []
        let calendar = Calendar.current
        let now = Date()
        if let laterToday = calendar.date(byAdding: .hour, value: 3, to: now),
           calendar.isDateInToday(laterToday) {
            presets.append((label: "Later today", symbol: "clock", date: laterToday))
        }
        if let tomorrow = calendar.date(byAdding: .day, value: 1, to: now) {
            if let morning = calendar.date(bySettingHour: 8, minute: 0, second: 0, of: tomorrow) {
                presets.append((label: "Tomorrow morning", symbol: "sunrise", date: morning))
            }
            if let afternoon = calendar.date(bySettingHour: 13, minute: 0, second: 0, of: tomorrow) {
                presets.append((label: "Tomorrow afternoon", symbol: "sun.max", date: afternoon))
            }
        }
        if let monday = calendar.nextDate(
            after: now.addingTimeInterval(24 * 3600),
            matching: DateComponents(hour: 8, minute: 0, weekday: 2),
            matchingPolicy: .nextTime
        ) {
            presets.append((label: "Monday morning", symbol: "briefcase", date: monday))
        }
        return presets.filter { Self.bounds.contains($0.date) }
    }
}
