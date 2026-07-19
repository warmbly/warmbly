import SwiftUI

/// Bottom-anchored undo-send capsule shown after an instant send: a live
/// countdown to `scheduledAt` (the backend queues instant sends a short
/// window into the future) plus an Undo button that cancels the queued task.
/// When the countdown hits zero it flips briefly to "Sent" and auto-dismisses
/// through `onExpired`. Styled after the unibox toast (dark capsule).
struct UndoSendBanner: View {
    let scheduledAt: Date
    /// Cancels the queued send; the caller owns dismissal and error handling.
    var onUndo: () async -> Void
    /// The window elapsed without an undo; the caller clears the banner state.
    var onExpired: () -> Void

    @State private var isCancelling = false
    @State private var sent = false

    /// Undo reads well on the dark capsule in both schemes (sky-300).
    private static let undoTint = Color(hex: 0x7DD3FC)

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: sent ? "checkmark.circle.fill" : "paperplane.fill")
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(sent ? WTheme.positive : Self.undoTint)
                .contentTransition(.symbolEffect(.replace))
            if sent {
                Text("Sent")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.white)
            } else {
                TimelineView(.periodic(from: .now, by: 0.5)) { timeline in
                    Text("Sending in \(remaining(at: timeline.date))s")
                        .font(.footnote.weight(.semibold))
                        .monospacedDigit()
                        .foregroundStyle(.white)
                        .contentTransition(.numericText(countsDown: true))
                }
                Rectangle()
                    .fill(.white.opacity(0.25))
                    .frame(width: 1, height: 16)
                Button {
                    guard !isCancelling else { return }
                    isCancelling = true
                    Task {
                        await onUndo()
                        isCancelling = false
                    }
                } label: {
                    Group {
                        if isCancelling {
                            ProgressView()
                                .controlSize(.small)
                                .tint(Self.undoTint)
                        } else {
                            Text("Undo")
                                .font(.footnote.weight(.bold))
                                .foregroundStyle(Self.undoTint)
                        }
                    }
                    .frame(minWidth: 36)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Undo send")
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 9)
        .background(.black.opacity(0.82), in: Capsule())
        .animation(.snappy, value: sent)
        .task(id: scheduledAt) {
            let delay = scheduledAt.timeIntervalSinceNow
            if delay > 0 {
                try? await Task.sleep(for: .seconds(delay))
            }
            guard !Task.isCancelled else { return }
            withAnimation(.snappy) { sent = true }
            try? await Task.sleep(for: .seconds(1.2))
            guard !Task.isCancelled else { return }
            onExpired()
        }
    }

    private func remaining(at date: Date) -> Int {
        max(0, Int(scheduledAt.timeIntervalSince(date).rounded(.up)))
    }
}
