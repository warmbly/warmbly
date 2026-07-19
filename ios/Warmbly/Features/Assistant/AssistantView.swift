import SwiftUI

/// The dashboard AI agent as a full-screen conversation: streaming answers,
/// tool steps as they run, approval cards for risky tools, and a history
/// sheet with search and grouped conversations. Entry is gated on `.useAI`.
struct AssistantView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    /// Context hint sent with the first message (web sends the current page).
    var page: String?

    @State private var chat = AssistantChatStore()
    @State private var input = ""
    @State private var showHistory = false
    @FocusState private var inputFocused: Bool

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                conversation
                if let pending = chat.pending {
                    approvalCard(pending)
                }
                if let error = chat.errorMessage {
                    errorBanner(error)
                }
                inputBar
            }
            .background(Color(.systemBackground))
            .navigationTitle(chat.title ?? "Assistant")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button {
                        dismiss()
                    } label: {
                        Image(systemName: "xmark")
                    }
                    .accessibilityLabel("Close assistant")
                }
                ToolbarItemGroup(placement: .topBarTrailing) {
                    Button {
                        showHistory = true
                    } label: {
                        Image(systemName: "clock.arrow.circlepath")
                    }
                    .accessibilityLabel("Conversation history")
                    Button {
                        withAnimation(.snappy) { chat.newChat() }
                        inputFocused = true
                    } label: {
                        Image(systemName: "square.and.pencil")
                    }
                    .disabled(chat.isEmpty)
                    .accessibilityLabel("New chat")
                }
            }
        }
        .sheet(isPresented: $showHistory) {
            AssistantHistoryView { session in
                showHistory = false
                Task { await chat.open(env.api, session: session) }
            }
        }
        .onDisappear { chat.stop() }
    }

    // MARK: Conversation

    @ViewBuilder
    private var conversation: some View {
        if chat.isLoadingTranscript {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if chat.turns.isEmpty {
            emptyState
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        ForEach(chat.turns) { turn in
                            turnView(turn)
                        }
                        if chat.isStreaming, awaitingFirstBlock {
                            thinkingRow
                        }
                        Color.clear.frame(height: 1).id("bottom")
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 14)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .scrollDismissesKeyboard(.interactively)
                .onChange(of: chat.turns.last?.blocks.last?.text) {
                    proxy.scrollTo("bottom", anchor: .bottom)
                }
                .onChange(of: chat.turns.count) {
                    withAnimation(.snappy) { proxy.scrollTo("bottom", anchor: .bottom) }
                }
            }
        }
    }

    /// True until the run's first block arrives, so the user sees activity.
    private var awaitingFirstBlock: Bool {
        guard let last = chat.turns.last else { return true }
        return last.role == "user" || last.blocks.isEmpty
    }

    private var thinkingRow: some View {
        HStack(spacing: 8) {
            Image(systemName: "sparkles")
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(Tone.indigo.color)
                .symbolEffect(.pulse)
            Text("Thinking…")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .skeletonShimmer()
        }
        .transition(.opacity)
    }

    private var emptyState: some View {
        VStack(spacing: 14) {
            Spacer()
            Image(systemName: "sparkles")
                .font(.system(size: 34, weight: .medium))
                .foregroundStyle(Tone.indigo.color)
            Text("Ask your workspace")
                .font(.title3.weight(.semibold))
            Text("Campaign performance, contact lookups, drafting help — the assistant can read and act on your workspace, and asks before anything risky.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 36)
            VStack(spacing: 8) {
                suggestionChip("How are my campaigns doing this week?")
                suggestionChip("Which mailboxes need attention?")
                suggestionChip("Draft a follow-up for my latest reply")
            }
            .padding(.top, 6)
            Spacer()
            Spacer()
        }
        .frame(maxWidth: .infinity)
    }

    private func suggestionChip(_ text: String) -> some View {
        Button {
            chat.send(env.api, text: text, page: page)
        } label: {
            Text(text)
                .font(.footnote.weight(.medium))
                .foregroundStyle(.primary)
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .background(Color(.secondarySystemBackground), in: Capsule())
        }
        .buttonStyle(TapScaleStyle())
    }

    // MARK: Turns

    @ViewBuilder
    private func turnView(_ turn: AssistantChatStore.Turn) -> some View {
        if turn.role == "user" {
            HStack {
                Spacer(minLength: 48)
                Text(turn.blocks.first?.text ?? "")
                    .font(.subheadline)
                    .foregroundStyle(.white)
                    .padding(.horizontal, 13)
                    .padding(.vertical, 9)
                    .background(WTheme.accent, in: RoundedRectangle(cornerRadius: 17, style: .continuous))
            }
        } else {
            VStack(alignment: .leading, spacing: 8) {
                ForEach(turn.blocks) { block in
                    blockView(block)
                }
            }
        }
    }

    @ViewBuilder
    private func blockView(_ block: AssistantChatStore.Block) -> some View {
        switch block.kind {
        case .text:
            Text(markdown(block.text) + (block.live ? AttributedString(" ▍") : AttributedString()))
                .font(.subheadline)
                .foregroundStyle(.primary)
                .frame(maxWidth: .infinity, alignment: .leading)
                .textSelection(.enabled)
        case .tool:
            toolChip(block)
        }
    }

    private func toolChip(_ block: AssistantChatStore.Block) -> some View {
        HStack(spacing: 8) {
            if block.done {
                Image(systemName: "checkmark.circle.fill")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Tone.emerald.color)
            } else {
                ProgressView().controlSize(.mini)
            }
            VStack(alignment: .leading, spacing: 1) {
                Text(AgentToolDisplay.label(block.tool))
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.primary)
                if let summary = block.argsSummary, !summary.isEmpty {
                    Text(summary)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    private func markdown(_ text: String) -> AttributedString {
        (try? AttributedString(
            markdown: text,
            options: AttributedString.MarkdownParsingOptions(
                interpretedSyntax: .inlineOnlyPreservingWhitespace
            )
        )) ?? AttributedString(text)
    }

    // MARK: Approval

    /// A risky tool paused the run: show what it wants to do and let the
    /// member approve once, always allow the tool, or deny it.
    private func approvalCard(_ pending: AgentPendingTool) -> some View {
        let tone: Tone = pending.risk == "high" ? .rose : pending.risk == "medium" ? .amber : .slate
        return VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "hand.raised.fill")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(tone.color)
                Text("Approval needed · \(AgentToolDisplay.label(pending.toolName))")
                    .font(.footnote.weight(.semibold))
                Spacer(minLength: 0)
                if let risk = pending.risk, !risk.isEmpty {
                    Text(risk.uppercased())
                        .font(.system(size: 10, weight: .bold))
                        .foregroundStyle(tone.color)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 2.5)
                        .background(tone.background, in: Capsule())
                }
            }
            if let summary = pending.argsSummary, !summary.isEmpty {
                Text(summary)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            HStack(spacing: 8) {
                Button {
                    chat.decide(env.api, decision: "approve")
                } label: {
                    Text("Approve")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.white)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 7)
                        .background(WTheme.accent, in: Capsule())
                }
                .buttonStyle(TapScaleStyle())
                Button {
                    chat.decide(env.api, decision: "always_allow")
                } label: {
                    Text("Always allow")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(WTheme.accent)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 7)
                        .background(Tone.sky.background, in: Capsule())
                }
                .buttonStyle(TapScaleStyle())
                Button {
                    chat.decide(env.api, decision: "deny")
                } label: {
                    Text("Deny")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(WTheme.negative)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 7)
                        .background(Tone.rose.background, in: Capsule())
                }
                .buttonStyle(TapScaleStyle())
                Spacer(minLength: 0)
            }
        }
        .padding(12)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .padding(.horizontal, 12)
        .padding(.bottom, 6)
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    private func errorBanner(_ error: String) -> some View {
        Text(error)
            .font(.footnote)
            .foregroundStyle(WTheme.negative)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 16)
            .padding(.vertical, 6)
    }

    // MARK: Input

    private var inputBar: some View {
        VStack(spacing: 0) {
            Divider()
            HStack(spacing: 10) {
                TextField("Ask anything…", text: $input, axis: .vertical)
                    .font(.subheadline)
                    .lineLimit(1 ... 4)
                    .focused($inputFocused)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 9)
                    .background(
                        Color(.secondarySystemBackground),
                        in: RoundedRectangle(cornerRadius: 18, style: .continuous)
                    )
                if chat.isStreaming {
                    Button {
                        chat.stop()
                    } label: {
                        Image(systemName: "stop.circle.fill")
                            .font(.system(size: 28))
                            .foregroundStyle(WTheme.negative)
                    }
                    .accessibilityLabel("Stop")
                } else {
                    Button {
                        let text = input
                        input = ""
                        chat.send(env.api, text: text, page: page)
                    } label: {
                        Image(systemName: "arrow.up.circle.fill")
                            .font(.system(size: 28))
                            .foregroundStyle(
                                input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                                    ? Color(.systemGray3) : WTheme.accent
                            )
                    }
                    .disabled(input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || chat.pending != nil)
                    .accessibilityLabel("Send")
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            if let footnote = inputFootnote {
                Text(footnote)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding(.bottom, 6)
            }
        }
        .background(.bar)
    }

    private var inputFootnote: String? {
        if chat.freeModel { return "Free model · no credits charged" }
        if let credits = chat.creditsRemaining { return "\(WFormat.compact(credits)) credits left" }
        return nil
    }
}

// MARK: - History

/// Conversation history sheet: grouped sections, title search, swipe delete,
/// and Clear history — the mobile mirror of the web history rail.
struct AssistantHistoryView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    var onSelect: (AgentSession) -> Void

    @State private var store = AssistantHistoryStore()
    @State private var confirmClear = false

    var body: some View {
        NavigationStack {
            @Bindable var store = store
            Group {
                if store.isLoading, !store.hasLoaded {
                    ProgressView()
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if let error = store.errorMessage {
                    EmptyStateView(title: "Couldn't load history", message: error)
                } else if store.filtered.isEmpty {
                    EmptyStateView(
                        title: store.query.isEmpty ? "No conversations yet" : "No matches",
                        message: store.query.isEmpty
                            ? "Conversations with the assistant show up here."
                            : "No conversation matches \"\(store.query)\"."
                    )
                } else {
                    List {
                        ForEach(store.grouped, id: \.title) { group in
                            Section {
                                ForEach(group.sessions) { session in
                                    row(session)
                                }
                            } header: {
                                EyebrowLabel(group.title)
                            }
                        }
                        if store.hasMore {
                            HStack {
                                Spacer()
                                ProgressView().controlSize(.small)
                                Spacer()
                            }
                            .onAppear { Task { await store.loadMore(env.api) } }
                        }
                    }
                    .listStyle(.plain)
                }
            }
            .navigationTitle("History")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(
                text: $store.query,
                placement: .navigationBarDrawer(displayMode: .always),
                prompt: "Search conversations"
            )
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { dismiss() }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Button(role: .destructive) {
                            confirmClear = true
                        } label: {
                            Label("Clear history", systemImage: "trash")
                        }
                    } label: {
                        Image(systemName: "ellipsis")
                    }
                    .disabled(store.sessions.isEmpty)
                }
            }
            .confirmationDialog(
                "Delete all your assistant conversations in this workspace? This can't be undone.",
                isPresented: $confirmClear,
                titleVisibility: .visible
            ) {
                Button("Clear history", role: .destructive) {
                    Task { await store.clearAll(env.api) }
                }
            }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .task { await store.load(env.api) }
        .onChange(of: env.realtime.pulse(for: .ai)) {
            Task { await store.load(env.api) }
        }
    }

    private func row(_ session: AgentSession) -> some View {
        Button {
            onSelect(session)
        } label: {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(session.displayTitle)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(.primary)
                        .lineLimit(1)
                    HStack(spacing: 6) {
                        // Owner attribution appears only with shared history on.
                        if let owner = session.userName, !owner.isEmpty {
                            Text(owner)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Text(UniboxFormat.listTime(session.updatedAt ?? session.createdAt))
                            .font(.caption)
                            .monospacedDigit()
                            .foregroundStyle(.tertiary)
                    }
                }
                Spacer(minLength: 8)
                if session.context?.pending != nil {
                    Image(systemName: "hand.raised.fill")
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(Tone.amber.color)
                        .accessibilityLabel("Waiting for approval")
                }
            }
            .padding(.vertical, 2)
        }
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) {
                Task { await store.delete(env.api, id: session.id) }
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
    }
}
