import SwiftUI

/// Remie on the phone, shaped like the agent apps people already know: glass
/// controls over the conversation, answers in full-width type, a neutral
/// bubble for what you asked, swipeable starters and a floating composer.
struct AssistantView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    /// Context hint sent with the first message (web sends the current page).
    var page: String?
    /// Sent as the first message when the view opens (Home's starter prompts).
    var initialPrompt: String?

    @State private var chat = AssistantChatStore()
    @State private var input = ""
    @State private var showHistory = false
    @State private var suggestions: [RemieSuggestion] = []
    @State private var appeared = false
    @State private var boop = 0
    @State private var sent = 0
    @State private var atBottom = true
    @FocusState private var inputFocused: Bool

    private struct Starter: Identifiable {
        let title: String
        let detail: String
        let prompt: String
        var dot: Color?
        var id: String { prompt }
    }

    private static let starters = [
        Starter(title: "Review my week", detail: "How are my campaigns doing?", prompt: "How are my campaigns doing this week?"),
        Starter(title: "Mailbox health", detail: "Which mailboxes need attention?", prompt: "Which mailboxes need attention?"),
        Starter(title: "Draft a follow-up", detail: "For my latest reply", prompt: "Draft a follow-up for my latest reply"),
        Starter(title: "Find warm leads", detail: "Who replied positively lately?", prompt: "Which contacts replied positively in the last week?"),
    ]

    private var starters: [Starter] {
        guard !suggestions.isEmpty else { return Self.starters }
        return suggestions.prefix(4).map { s in
            Starter(title: s.text, detail: Self.severityLabel(s.severity), prompt: s.prompt, dot: s.dot)
        }
    }

    private static func severityLabel(_ severity: String) -> String {
        switch severity {
        case "critical": "Critical · ask Remie to fix it"
        case "high": "High impact · ask Remie to fix it"
        default: "Ask Remie to fix it"
        }
    }

    /// The conversation and composer keep a reading width on a wide window.
    private static let readableWidth: CGFloat = 760

    var body: some View {
        NavigationStack {
            content
                .background(WTheme.canvas)
                .safeAreaInset(edge: .bottom, spacing: 0) { bottomBar }
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { toolbar }
        }
        .tint(WTheme.ink)
        .sheet(isPresented: $showHistory) {
            AssistantHistoryView { session in
                showHistory = false
                Task { await chat.open(env.api, session: session) }
            }
        }
        .onDisappear { chat.stop() }
        .task {
            guard let initialPrompt, chat.isEmpty else { return }
            #if DEBUG
            if initialPrompt.hasPrefix("demo") {
                chat.playDemo(approval: initialPrompt == "demo-approval")
                return
            }
            #endif
            chat.send(env.api, text: initialPrompt, page: page)
        }
        .task { await loadSuggestions() }
        .onAppear {
            withAnimation(.spring(response: 0.6, dampingFraction: 0.86)) { appeared = true }
        }
        .sensoryFeedback(.impact(weight: .light), trigger: sent)
        .sensoryFeedback(trigger: chat.isStreaming) { was, now in
            was && !now && chat.errorMessage == nil ? .success : nil
        }
    }

    private var markState: RemieMarkState {
        if chat.pending != nil { return .attention }
        return chat.isStreaming ? .thinking : .idle
    }

    // MARK: Toolbar

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .topBarLeading) {
            Button {
                dismiss()
            } label: {
                Image(systemName: "chevron.down")
            }
            .accessibilityLabel("Close Remie")
        }
        ToolbarItem(placement: .principal) {
            HStack(spacing: 7) {
                RemieMark(size: 20, state: markState, celebrate: chat.errorMessage == nil, boop: boop)
                Text(chat.title.flatMap { $0.isEmpty ? nil : $0 } ?? "Remie")
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                    .contentTransition(.opacity)
            }
            .frame(maxWidth: 220)
            .animation(.easeOut(duration: 0.2), value: chat.title)
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

    // MARK: Content

    @ViewBuilder
    private var content: some View {
        if chat.isLoadingTranscript {
            loadingSkeleton
        } else if chat.turns.isEmpty, !chat.isStreaming {
            emptyState
        } else {
            conversation
        }
    }

    private var conversation: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 22) {
                    ForEach(chat.turns) { turn in
                        turnView(turn, streaming: chat.isStreaming && chat.pending == nil && turn.id == chat.turns.last?.id)
                    }
                    if let pending = chat.pending {
                        RemieApprovalCard(pending: pending) { decision in
                            chat.decide(env.api, decision: decision)
                        }
                        .transition(.asymmetric(
                            insertion: .scale(scale: 0.96, anchor: .bottom).combined(with: .opacity).combined(with: .offset(y: 10)),
                            removal: .opacity.combined(with: .scale(scale: 0.98))
                        ))
                    }
                    if chat.isStreaming, chat.pending == nil {
                        RemieWorkingStatus(
                            label: phase,
                            since: chat.runStartedAt,
                            step: chat.iteration,
                            budget: chat.budget
                        )
                        .transition(.opacity.combined(with: .offset(y: 4)))
                    }
                    if let error = chat.errorMessage {
                        errorRow(error)
                    }
                    Color.clear.frame(height: 1).id("bottom")
                }
                .padding(.horizontal, 20)
                .padding(.top, 12)
                .padding(.bottom, 24)
                .frame(maxWidth: .infinity, alignment: .leading)
                .animation(.spring(response: 0.42, dampingFraction: 0.86), value: chat.pending?.toolCallID)
                .animation(.easeOut(duration: 0.2), value: chat.isStreaming)
            }
            .defaultScrollAnchor(.bottom)
            .defaultScrollAnchor(.bottom, for: .sizeChanges)
            .readableMargins(Self.readableWidth)
            .scrollEdgeEffectStyle(.soft, for: .vertical)
            .scrollDismissesKeyboard(.interactively)
            .onScrollGeometryChange(for: Bool.self) { geo in
                geo.contentSize.height <= geo.containerSize.height || geo.visibleRect.maxY >= geo.contentSize.height - 80
            } action: { _, isAtBottom in
                withAnimation(.snappy) { atBottom = isAtBottom }
            }
            .onChange(of: chat.turns.count) {
                withAnimation(.snappy) { proxy.scrollTo("bottom", anchor: .bottom) }
            }
            .overlay(alignment: .bottom) {
                if !atBottom {
                    Button {
                        withAnimation(.snappy) { proxy.scrollTo("bottom", anchor: .bottom) }
                    } label: {
                        Image(systemName: "arrow.down")
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundStyle(WTheme.ink)
                            .frame(width: 40, height: 40)
                    }
                    .buttonStyle(TapScaleStyle())
                    .glassEffect(.regular.interactive(), in: Circle())
                    .padding(.bottom, 10)
                    .transition(.scale(scale: 0.6).combined(with: .opacity))
                    .accessibilityLabel("Jump to latest")
                }
            }
        }
    }

    /// What the live run is doing, read off the tail of the transcript.
    private var phase: String {
        guard let last = chat.turns.last, last.role == "assistant", let block = last.blocks.last else {
            return "Thinking"
        }
        if block.kind == .tool, !block.done { return "Working" }
        if block.kind == .text, block.live { return "Writing" }
        return "Thinking"
    }

    private var loadingSkeleton: some View {
        VStack(alignment: .leading, spacing: 22) {
            HStack {
                Spacer()
                RoundedRectangle(cornerRadius: 20, style: .continuous)
                    .fill(WTheme.fill)
                    .frame(width: 170, height: 42)
            }
            // Sized off the column, not the window, so it matches the transcript that replaces it.
            GeometryReader { proxy in
                VStack(alignment: .leading, spacing: 10) {
                    ForEach([0.94, 0.82, 0.58], id: \.self) { width in
                        RoundedRectangle(cornerRadius: 5)
                            .fill(WTheme.fill)
                            .frame(width: proxy.size.width * width, height: 13)
                    }
                }
            }
            .frame(height: 59)
            Spacer()
        }
        .skeletonShimmer()
        .padding(20)
        .frame(maxWidth: Self.readableWidth)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
    }

    // MARK: Turns

    @ViewBuilder
    private func turnView(_ turn: AssistantChatStore.Turn, streaming: Bool) -> some View {
        if turn.role == "user" {
            HStack {
                Spacer(minLength: 60)
                Text(turn.blocks.map(\.text).joined())
                    .font(.system(size: 16))
                    .lineSpacing(3)
                    .foregroundStyle(WTheme.ink)
                    .textSelection(.enabled)
                    .padding(.horizontal, 15)
                    .padding(.vertical, 10)
                    .background(WTheme.fill, in: RoundedRectangle(cornerRadius: 21, style: .continuous))
            }
            .transition(.asymmetric(
                insertion: .scale(scale: 0.94, anchor: .bottomTrailing)
                    .combined(with: .opacity)
                    .combined(with: .offset(y: 10)),
                removal: .opacity
            ))
        } else {
            let segments = Self.segments(turn.blocks)
            let lastIndex = turn.blocks.count - 1
            let finished = !streaming && !(chat.isStreaming && turn.id == chat.turns.last?.id)
            let answer = turn.blocks.filter { $0.kind == .text }.map(\.text).joined(separator: "\n\n")
            VStack(alignment: .leading, spacing: 12) {
                ForEach(segments) { segment in
                    switch segment.kind {
                    case let .tools(steps):
                        RemieActivityTrace(
                            steps: steps,
                            live: streaming && segment.id == segments.last?.id,
                            streaming: chat.isStreaming
                        )
                    case let .text(block, index):
                        if !block.text.isEmpty {
                            if streaming && index == lastIndex {
                                RemieSmoothText(text: block.text)
                            } else {
                                RemieMarkdown(text: block.text)
                            }
                        }
                    }
                }
                if finished, !answer.isEmpty {
                    RemieTurnActions(text: answer)
                        .transition(.opacity)
                }
            }
            .transition(.opacity)
        }
    }

    private struct Segment: Identifiable {
        enum Kind {
            case tools([AssistantChatStore.Block])
            case text(AssistantChatStore.Block, Int)
        }

        let id: UUID
        var kind: Kind
    }

    /// Folds consecutive tool calls into one trace, keyed by its first step so
    /// a trace keeps its open state as steps stream in.
    private static func segments(_ blocks: [AssistantChatStore.Block]) -> [Segment] {
        var out: [Segment] = []
        for (index, block) in blocks.enumerated() {
            guard block.kind == .tool else {
                out.append(Segment(id: block.id, kind: .text(block, index)))
                continue
            }
            if let last = out.indices.last, case var .tools(steps) = out[last].kind {
                steps.append(block)
                out[last].kind = .tools(steps)
            } else {
                out.append(Segment(id: block.id, kind: .tools([block])))
            }
        }
        return out
    }

    private func errorRow(_ message: String) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "exclamationmark.circle.fill")
                .font(.system(size: 16))
                .foregroundStyle(WTheme.negative)
            VStack(alignment: .leading, spacing: 2) {
                Text("Remie couldn't finish")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(WTheme.ink)
                Text(message.prefix(1).uppercased() + message.dropFirst())
                    .font(.system(size: 14))
                    .foregroundStyle(WTheme.inkSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
        .padding(14)
        .background(WTheme.negative.opacity(0.07), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        .transition(.opacity.combined(with: .offset(y: 6)))
    }

    // MARK: Empty state

    private var emptyState: some View {
        ScrollView {
            VStack(spacing: 0) {
                ZStack {
                    Circle()
                        .fill(RadialGradient(
                            colors: [Remie.sky500.opacity(0.18), Remie.sky500.opacity(0)],
                            center: .center,
                            startRadius: 0,
                            endRadius: 110
                        ))
                        .frame(width: 220, height: 220)
                        .scaleEffect(appeared ? 1 : 0.6)
                    RemieMark(size: 76, boop: boop, tappable: true)
                        .scaleEffect(appeared ? 1 : 0.7)
                }
                .frame(height: 170)
                Text("Hi, I'm Remie")
                    .displayFont(30, weight: .semibold)
                    .foregroundStyle(WTheme.ink)
                Text("I look into your outreach and fix things for you. I always ask before changing anything.")
                    .font(.system(size: 16))
                    .lineSpacing(3)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(WTheme.inkSecondary)
                    .frame(maxWidth: 310)
                    .padding(.top, 10)
            }
            .opacity(appeared ? 1 : 0)
            .offset(y: appeared ? 0 : 12)
            .padding(.horizontal, 24)
            .frame(maxWidth: .infinity)
            .containerRelativeFrame(.vertical, alignment: .center) { length, _ in length }
        }
        .scrollBounceBehavior(.basedOnSize)
        .scrollDismissesKeyboard(.interactively)
    }

    /// Swipeable starters above the composer: the Advisor's open problems when
    /// there are any, else everyday asks.
    private var starterCards: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 10) {
                ForEach(Array(starters.enumerated()), id: \.element.id) { index, starter in
                    Button {
                        send(starter.prompt)
                    } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            HStack(spacing: 7) {
                                if let dot = starter.dot {
                                    Circle().fill(dot).frame(width: 7, height: 7)
                                }
                                Text(starter.title)
                                    .font(.system(size: 15, weight: .semibold))
                                    .foregroundStyle(WTheme.ink)
                                    .lineLimit(1)
                            }
                            Text(starter.detail)
                                .font(.system(size: 13.5))
                                .foregroundStyle(WTheme.inkSecondary)
                                .lineLimit(1)
                        }
                        .frame(width: 210, alignment: .leading)
                        .padding(.horizontal, 15)
                        .padding(.vertical, 12)
                        .background(WTheme.surface, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
                    }
                    .buttonStyle(TapScaleStyle())
                    .opacity(appeared ? 1 : 0)
                    .offset(x: appeared ? 0 : 24)
                    .animation(
                        .spring(response: 0.5, dampingFraction: 0.86).delay(0.12 + Double(index) * 0.06),
                        value: appeared
                    )
                }
            }
            .padding(.horizontal, 16)
        }
        .scrollClipDisabled()
        .animation(.easeOut(duration: 0.25), value: suggestions.count)
    }

    private func loadSuggestions() async {
        guard env.session.can(.viewAnalytics) else { return }
        struct Page: Decodable { var data: [RemieAdvisorFinding]? }
        guard let page: Page = try? await env.api.get("advisor/recommendations", query: ["limit": "50"]) else { return }
        withAnimation(.easeOut(duration: 0.2)) {
            suggestions = RemieSuggestion.from(page.data ?? [])
        }
    }

    // MARK: Composer

    private var canSend: Bool {
        !input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && chat.pending == nil && !chat.isStreaming
    }

    private func send(_ text: String) {
        guard chat.pending == nil, !chat.isStreaming else { return }
        sent += 1
        chat.send(env.api, text: text, page: page)
    }

    private var bottomBar: some View {
        VStack(spacing: 10) {
            if chat.turns.isEmpty, !chat.isStreaming, !chat.isLoadingTranscript {
                starterCards
                    .transition(.opacity.combined(with: .offset(y: 8)))
            }
            composer
                .padding(.horizontal, 12)
            footnote
                .padding(.horizontal, 20)
        }
        .frame(maxWidth: Self.readableWidth)
        .frame(maxWidth: .infinity)
        .padding(.top, 8)
        .padding(.bottom, 4)
        .animation(.spring(response: 0.4, dampingFraction: 0.88), value: chat.turns.isEmpty)
    }

    private var composer: some View {
        HStack(alignment: .bottom, spacing: 10) {
            TextField(
                chat.pending != nil ? "Answer the approval first" : "Ask Remie anything",
                text: $input,
                axis: .vertical
            )
            .font(.system(size: 16.5))
            .foregroundStyle(WTheme.ink)
            .lineLimit(1 ... 6)
            .focused($inputFocused)
            .disabled(chat.pending != nil)
            .padding(.vertical, 11)
            .onChange(of: input) { old, new in
                if new.count > old.count { boop += 1 }
            }

            composerButton
                .padding(.bottom, 6)
        }
        .padding(.leading, 18)
        .padding(.trailing, 6)
        .glassEffect(.regular.interactive(), in: RoundedRectangle(cornerRadius: 26, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: 26, style: .continuous))
        .onTapGesture { inputFocused = true }
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: chat.isStreaming)
        .animation(.spring(response: 0.3, dampingFraction: 0.8), value: canSend)
    }

    @ViewBuilder
    private var composerButton: some View {
        if chat.isStreaming {
            Button {
                chat.stop()
            } label: {
                Image(systemName: "square.fill")
                    .font(.system(size: 11, weight: .bold))
                    .foregroundStyle(WTheme.inkInverse)
                    .frame(width: 36, height: 36)
                    .background(WTheme.ink, in: Circle())
            }
            .buttonStyle(TapScaleStyle())
            .accessibilityLabel("Stop")
            .transition(.scale(scale: 0.5).combined(with: .opacity))
        } else {
            Button {
                let text = input
                input = ""
                send(text)
            } label: {
                Image(systemName: "arrow.up")
                    .font(.system(size: 15, weight: .bold))
                    .foregroundStyle(canSend ? WTheme.inkInverse : WTheme.inkTertiary)
                    .frame(width: 36, height: 36)
                    .background(canSend ? WTheme.ink : WTheme.fill, in: Circle())
            }
            .buttonStyle(TapScaleStyle())
            .disabled(!canSend)
            .accessibilityLabel("Send")
            .transition(.scale(scale: 0.5).combined(with: .opacity))
        }
    }

    private var footnote: some View {
        HStack(spacing: 6) {
            if chat.freeModel {
                Text("Free local model, nothing is charged")
            } else {
                Text("Reads run on their own. Changes ask you first.")
                if let credits = chat.creditsRemaining {
                    Text("·")
                    Text("\(credits.formatted()) credits")
                        .monospacedDigit()
                        .contentTransition(.numericText())
                }
            }
        }
        .font(.system(size: 11.5))
        .foregroundStyle(WTheme.inkTertiary)
        .lineLimit(1)
        .minimumScaleFactor(0.85)
        .frame(maxWidth: .infinity)
    }
}

// MARK: - History

/// Conversation history sheet: grouped hairline rows, title search, swipe
/// delete, and Clear history. The mobile mirror of the web history rail.
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
                        .tint(WTheme.inkSecondary)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if let error = store.errorMessage {
                    EmptyStateView(title: "Couldn't load history", message: error)
                } else if store.filtered.isEmpty {
                    EmptyStateView(
                        title: store.query.isEmpty ? "No conversations yet" : "No matches",
                        message: store.query.isEmpty
                            ? "Conversations with the assistant show up here."
                            : "No conversation matches \"\(store.query)\".",
                        symbol: "sparkle"
                    )
                } else {
                    List {
                        ForEach(store.grouped, id: \.title) { group in
                            Section {
                                ForEach(group.sessions) { session in
                                    row(session)
                                        .listRowInsets(EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20))
                                        .listRowBackground(WTheme.canvas)
                                        .listRowSeparatorTint(WTheme.hairline)
                                }
                            } header: {
                                EyebrowLabel(group.title)
                                    .padding(.top, 10)
                                    .padding(.bottom, 2)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .background(WTheme.canvas)
                                    .listRowInsets(EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20))
                            }
                        }
                        if store.hasMore {
                            HStack {
                                Spacer()
                                ProgressView().controlSize(.small)
                                Spacer()
                            }
                            .listRowBackground(WTheme.canvas)
                            .listRowSeparator(.hidden)
                            .onAppear { Task { await store.loadMore(env.api) } }
                        }
                    }
                    .listStyle(.plain)
                    .scrollContentBackground(.hidden)
                    .environment(\.defaultMinListRowHeight, 0)
                }
            }
            .background(WTheme.canvas)
            .navigationTitle("History")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(WTheme.canvas, for: .navigationBar)
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
        .tint(WTheme.ink)
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .presentationBackground(WTheme.canvas)
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
                VStack(alignment: .leading, spacing: 4) {
                    Text(session.displayTitle)
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                    HStack(spacing: 6) {
                        // Owner attribution appears only with shared history on.
                        if let owner = session.userName, !owner.isEmpty {
                            Text(owner)
                                .font(.system(size: 12.5))
                                .foregroundStyle(WTheme.inkSecondary)
                                .lineLimit(1)
                        }
                        Text(UniboxFormat.listTime(session.updatedAt ?? session.createdAt))
                            .font(WFont.meta(11.5))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                }
                Spacer(minLength: 8)
                if session.context?.pending != nil {
                    Image(systemName: "hand.raised")
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(Tone.amber.color)
                        .accessibilityLabel("Waiting for approval")
                }
                Image(systemName: "chevron.right")
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(WTheme.inkTertiary)
            }
            .padding(.vertical, 12)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) {
                Task { await store.delete(env.api, id: session.id) }
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
    }
}
