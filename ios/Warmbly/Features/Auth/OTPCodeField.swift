import SwiftUI

/// Six code boxes backed by one invisible text field, so iOS one-time-code
/// autofill (from Mail/Messages) fills the whole row in a single tap.
struct OTPCodeField: View {
    @Binding var code: String
    var boxCount: Int = 6
    var onComplete: (String) -> Void = { _ in }

    @FocusState private var focused: Bool

    var body: some View {
        ZStack {
            TextField("", text: $code)
                .keyboardType(.numberPad)
                .textContentType(.oneTimeCode)
                .focused($focused)
                .opacity(0.02)
                .allowsHitTesting(false)
                .onChange(of: code) { _, newValue in
                    let digits = String(newValue.filter(\.isNumber).prefix(boxCount))
                    if digits != newValue { code = digits }
                    if digits.count == boxCount { onComplete(digits) }
                }

            HStack(spacing: 7) {
                ForEach(0 ..< boxCount, id: \.self) { index in
                    box(at: index)
                }
            }
            .frame(maxWidth: .infinity)
        }
        .contentShape(Rectangle())
        .onTapGesture { focused = true }
        .onAppear { focused = true }
    }

    private func box(at index: Int) -> some View {
        let digits = Array(code)
        let char = index < digits.count ? String(digits[index]) : ""
        let isActive = focused && index == min(code.count, boxCount - 1)

        return Text(char)
            .font(.system(size: 27, weight: .semibold, design: .monospaced))
            .frame(width: 50, height: 62)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 14))
            .overlay(
                RoundedRectangle(cornerRadius: 14)
                    .strokeBorder(isActive ? WTheme.accent : Color(.separator).opacity(0.4), lineWidth: isActive ? 2 : 1)
            )
            .scaleEffect(char.isEmpty ? 1 : 1.02)
            .animation(.easeOut(duration: 0.15), value: isActive)
            .animation(.spring(response: 0.25, dampingFraction: 0.6), value: char)
    }
}
