import SwiftUI

/// Chip-based recipient editor with contact autocomplete, shared by the
/// compose window and the reply composer (web ContactRecipientField parity).
/// Committed addresses render as removable chips; typing searches contacts by
/// name, email, or company, and a query that matches a contact category
/// offers it as a filter that narrows the suggestions to that group.
///
/// The parent owns both the chips and the in-progress text so a send can
/// include an address the user typed but never committed.
struct RecipientField<Trailing: View>: View {
    @Environment(AppEnvironment.self) private var env

    let label: String
    @Binding var addresses: [String]
    @Binding var text: String
    @ViewBuilder var trailing: () -> Trailing

    @State private var suggestions: [Contact] = []
    @State private var categories: [ContactCategory] = []
    @State private var activeCategory: ContactCategory?
    @State private var showPicker = false
    @FocusState private var focused: Bool

    private var canSearch: Bool { env.session.can(.viewContacts) }

    private var query: String { text.trimmingCharacters(in: .whitespaces) }

    /// Categories whose title matches the query (offered as filter rows).
    private var categoryMatches: [ContactCategory] {
        guard query.count >= 2, activeCategory == nil else { return [] }
        let lowered = query.lowercased()
        return categories
            .filter { ($0.title ?? "").lowercased().contains(lowered) }
            .prefix(2)
            .map { $0 }
    }

    private var showsSuggestions: Bool {
        focused && (!suggestions.isEmpty || !categoryMatches.isEmpty)
    }

    var body: some View {
        VStack(spacing: 0) {
            row
            if showsSuggestions {
                suggestionList
                    .transition(.opacity)
            }
        }
        .animation(.snappy, value: showsSuggestions)
        .task {
            guard canSearch, categories.isEmpty else { return }
            if let me: ContactsMePayload = try? await env.api.get("auth/me") {
                categories = me.categories ?? []
            }
        }
        .task(id: "\(query)|\(activeCategory?.id ?? "")") {
            await search()
        }
        .onChange(of: focused) {
            // Leaving the field commits what was typed, like the web chips.
            if !focused { commitText() }
        }
    }

    // MARK: Row

    private var row: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 44, alignment: .leading)
            RecipientFlow(spacing: 6) {
                if let activeCategory {
                    categoryFilterChip(activeCategory)
                }
                ForEach(addresses, id: \.self) { address in
                    chip(address)
                }
                TextField(addresses.isEmpty && activeCategory == nil ? "" : " ", text: $text)
                    .font(.subheadline)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.emailAddress)
                    .submitLabel(.return)
                    .focused($focused)
                    .onSubmit {
                        commitText()
                        focused = true
                    }
                    .onChange(of: text) { commitSeparators() }
                    .frame(minWidth: 80)
            }
            if canSearch {
                Button {
                    showPicker = true
                } label: {
                    Image(systemName: "plus.circle")
                        .font(.system(size: 17, weight: .medium))
                        .foregroundStyle(.secondary)
                        .frame(width: 28, height: 28)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Choose contacts")
                .sheet(isPresented: $showPicker) {
                    ContactPickerSheet(existing: addresses) { emails in
                        for email in emails { append(email) }
                    }
                }
            }
            trailing()
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .frame(minHeight: 46, alignment: .center)
        .contentShape(Rectangle())
        .onTapGesture { focused = true }
    }

    private func chip(_ address: String) -> some View {
        Button {
            withAnimation(.snappy) { addresses.removeAll { $0 == address } }
        } label: {
            HStack(spacing: 4) {
                Text(address)
                    .font(.footnote.weight(.medium))
                    .lineLimit(1)
                Image(systemName: "xmark")
                    .font(.system(size: 8, weight: .bold))
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 9)
            .padding(.vertical, 4.5)
            .background(Color(.secondarySystemBackground), in: Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Remove \(address)")
    }

    private func categoryFilterChip(_ category: ContactCategory) -> some View {
        let tint = Color(uniboxHex: category.color) ?? WTheme.accent
        return Button {
            withAnimation(.snappy) { activeCategory = nil }
        } label: {
            HStack(spacing: 4) {
                Circle().fill(tint).frame(width: 7, height: 7)
                Text(category.title ?? "Category")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(tint)
                    .lineLimit(1)
                Image(systemName: "xmark")
                    .font(.system(size: 8, weight: .bold))
                    .foregroundStyle(tint.opacity(0.6))
            }
            .padding(.horizontal, 9)
            .padding(.vertical, 4.5)
            .background(tint.opacity(0.12), in: Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Clear \(category.title ?? "category") filter")
    }

    // MARK: Suggestions

    private var suggestionList: some View {
        VStack(spacing: 0) {
            Divider()
            ForEach(categoryMatches) { category in
                categoryRow(category)
                Divider().padding(.leading, 56)
            }
            ForEach(suggestions.prefix(6)) { contact in
                contactRow(contact)
                Divider().padding(.leading, 56)
            }
        }
        .background(Color(.systemBackground))
    }

    private func categoryRow(_ category: ContactCategory) -> some View {
        let tint = Color(uniboxHex: category.color) ?? WTheme.accent
        return Button {
            withAnimation(.snappy) {
                activeCategory = category
                text = ""
            }
            focused = true
        } label: {
            HStack(spacing: 12) {
                Image(systemName: "line.3.horizontal.decrease.circle")
                    .font(.system(size: 17, weight: .medium))
                    .foregroundStyle(tint)
                    .frame(width: 28)
                Text("Filter by ")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    + Text(category.title ?? "category")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(tint)
                Spacer(minLength: 8)
            }
            .padding(.horizontal, 16)
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(TapScaleStyle())
    }

    private func contactRow(_ contact: Contact) -> some View {
        Button {
            select(contact)
        } label: {
            HStack(spacing: 12) {
                WAvatar(name: contact.displayName, seed: contact.id, size: 28)
                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 6) {
                        Text(contact.hasName ? contact.displayName : (contact.email ?? ""))
                            .font(.subheadline.weight(.medium))
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                        ForEach((contact.categories ?? []).prefix(2)) { category in
                            categoryDot(category)
                        }
                    }
                    if let detail = contactDetailLine(contact) {
                        Text(detail)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
            }
            .padding(.horizontal, 16)
            .frame(minHeight: 46)
            .contentShape(Rectangle())
        }
        .buttonStyle(TapScaleStyle())
    }

    private func categoryDot(_ category: ContactMiniCategory) -> some View {
        let tint = Color(uniboxHex: category.color) ?? WTheme.accent
        return HStack(spacing: 3) {
            Circle().fill(tint).frame(width: 6, height: 6)
            Text(category.title ?? "")
                .font(.system(size: 10.5, weight: .medium))
                .foregroundStyle(tint)
                .lineLimit(1)
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(tint.opacity(0.1), in: Capsule())
    }

    private func contactDetailLine(_ contact: Contact) -> String? {
        var parts: [String] = []
        if contact.hasName, let email = contact.email, !email.isEmpty { parts.append(email) }
        if let company = contact.company, !company.isEmpty { parts.append(company) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    // MARK: Behavior

    private func search() async {
        guard canSearch else { return }
        let wantsResults = query.count >= 2 || activeCategory != nil
        guard wantsResults else {
            if !suggestions.isEmpty { suggestions = [] }
            return
        }
        try? await Task.sleep(for: .milliseconds(250))
        guard !Task.isCancelled else { return }
        var body = ContactSearchBody()
        if !query.isEmpty { body.query = query }
        if let activeCategory { body.categoryIDs = [activeCategory.id] }
        guard let page: ContactSearchPage = try? await env.api.post(
            "contacts/search", body: body, query: ["limit": "8"]
        ), !Task.isCancelled else { return }
        let taken = Set(addresses.map { $0.lowercased() })
        suggestions = page.data.filter { contact in
            guard let email = contact.email, !email.isEmpty else { return false }
            return !taken.contains(email.lowercased())
        }
    }

    private func select(_ contact: Contact) {
        guard let email = contact.email, !email.isEmpty else { return }
        append(email)
        text = ""
        focused = true
    }

    /// Splitting on `, ; \n` commits complete parts as chips as they're typed.
    private func commitSeparators() {
        guard text.contains(where: { $0 == "," || $0 == ";" || $0 == "\n" }) else { return }
        var parts = text.split(whereSeparator: { $0 == "," || $0 == ";" || $0 == "\n" }).map(String.init)
        let endsWithSeparator = text.last.map { $0 == "," || $0 == ";" || $0 == "\n" } ?? false
        let remainder = endsWithSeparator ? "" : (parts.popLast() ?? "")
        for part in parts { append(part) }
        text = remainder
    }

    private func commitText() {
        let pending = text
        text = ""
        for part in pending.split(whereSeparator: { $0 == "," || $0 == ";" || $0 == "\n" }) {
            append(String(part))
        }
    }

    private func append(_ raw: String) {
        let address = UniboxAddress.bare(raw.trimmingCharacters(in: .whitespaces))
        guard !address.isEmpty else { return }
        guard !addresses.contains(where: { $0.lowercased() == address.lowercased() }) else { return }
        withAnimation(.snappy) { addresses.append(address) }
    }
}

extension RecipientField where Trailing == EmptyView {
    init(label: String, addresses: Binding<[String]>, text: Binding<String>) {
        self.init(label: label, addresses: addresses, text: text, trailing: { EmptyView() })
    }
}

// MARK: - Flow layout

/// Minimal wrapping layout for recipient chips + the inline text field.
struct RecipientFlow: Layout {
    var spacing: CGFloat = 6

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, rowHeight: CGFloat = 0
        for subview in subviews {
            let size = subview.sizeThatFits(.unspecified)
            if x > 0, x + size.width > width {
                x = 0
                y += rowHeight + spacing
                rowHeight = 0
            }
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
        return CGSize(width: width == .infinity ? x : width, height: y + rowHeight)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, rowHeight: CGFloat = 0
        for subview in subviews {
            let size = subview.sizeThatFits(.unspecified)
            if x > bounds.minX, x + size.width > bounds.maxX {
                x = bounds.minX
                y += rowHeight + spacing
                rowHeight = 0
            }
            subview.place(
                at: CGPoint(x: x, y: y),
                proposal: ProposedViewSize(width: min(size.width, bounds.width), height: size.height)
            )
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
    }
}
