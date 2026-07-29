import SwiftUI

// Stores for the CRM + templates screens. Each is @MainActor @Observable, loads
// via async APIClient methods, keeps stale data during reloads, and exposes a
// single first-load flag so views can show SkeletonRows only on the first pass.

// MARK: - Deals

@MainActor
@Observable
final class CRMDealsStore {
    private(set) var pipelines: [CRMPipeline] = []
    private(set) var deals: [CRMDeal] = []
    private(set) var summary: CRMDealsSummary?
    private(set) var totalCount: Int?
    private(set) var isLoading = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    /// nil = all pipelines; otherwise the selected pipeline id.
    var pipelineID: String?
    /// nil = every status; otherwise "open" | "won" | "lost".
    var statusFilter: String?
    var query = ""

    private var generation = 0

    var selectedPipeline: CRMPipeline? {
        guard let pipelineID else { return nil }
        return pipelines.first { $0.id == pipelineID }
    }

    /// Stages of the active pipeline, ordered, for grouping the list.
    var orderedStages: [CRMStage] {
        selectedPipeline?.orderedStages ?? []
    }

    func stage(_ id: String?) -> CRMStage? {
        guard let id else { return nil }
        for pipeline in pipelines {
            if let match = pipeline.stages?.first(where: { $0.id == id }) { return match }
        }
        return nil
    }

    /// Currency shown in the stat strip: summary's, else the first deal's.
    var displayCurrency: String? {
        summary?.currency ?? deals.first?.currency
    }

    /// Drawer scope count for a status filter (nil = all). The summary is
    /// requested without the status filter so these stay stable across scopes.
    func statusCount(_ status: String?) -> Int? {
        guard let summary else { return nil }
        switch status {
        case "open": return summary.openCount
        case "won": return summary.wonCount
        case "lost": return summary.lostCount
        default: return summary.total
        }
    }

    /// Open deals in a pipeline, summed from the stage counts when present.
    func pipelineDealCount(_ pipeline: CRMPipeline) -> Int? {
        let counts = (pipeline.stages ?? []).compactMap(\.dealCount)
        guard !counts.isEmpty else { return nil }
        return counts.reduce(0, +)
    }

    private func searchBody() -> CRMDealSearchBody {
        var body = CRMDealSearchBody()
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        if !trimmed.isEmpty { body.query = trimmed }
        if let statusFilter { body.statuses = [statusFilter] }
        if let pipelineID { body.pipelineIDs = [pipelineID] }
        body.sortBy = "created_at"
        return body
    }

    func load(_ api: APIClient) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            // Pipelines are a bare array; fetch them first so grouping works.
            let pipes: [CRMPipeline] = try await api.get("crm/pipelines")
            guard gen == generation else { return }
            let ordered = pipes.sorted { ($0.position ?? 0) < ($1.position ?? 0) }
            // Default to the first pipeline if nothing valid is selected.
            if pipelineID == nil || !ordered.contains(where: { $0.id == pipelineID }) {
                pipelineID = ordered.first?.id
            }

            let body = searchBody()
            // Summary without status/query so drawer scope counts stay stable
            // while a scope or search is active (pipeline-scoped only).
            var unfiltered = body
            unfiltered.statuses = nil
            unfiltered.query = nil
            let summaryBody = unfiltered
            async let dealsPage: CRMPage<CRMDeal> = api.post("crm/deals/search", body: body, query: ["limit": "200"])
            async let summaryResult: CRMDealsSummary = api.post("crm/deals/summary", body: summaryBody)
            let page = try await dealsPage
            let summaryValue = try? await summaryResult
            guard gen == generation else { return }

            withAnimation(.snappy) {
                pipelines = ordered
                deals = page.items
                totalCount = (page.pagination?.total).map(Int.init)
                summary = summaryValue
            }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
        } catch {
            guard gen == generation else { return }
            if !hasLoaded { errorMessage = error.localizedDescription }
            isLoading = false
        }
    }

    /// Deals in a stage (open only; won/lost drop out of stage columns).
    func deals(inStage stageID: String) -> [CRMDeal] {
        deals.filter { $0.stageID == stageID && ($0.status ?? "open") == "open" }
    }

    var closedDeals: [CRMDeal] {
        deals.filter { ($0.status ?? "open") != "open" }
    }

    func update(_ api: APIClient, deal: CRMDeal, body: CRMDealUpdateBody) async throws {
        let updated: CRMDeal = try await api.patch("crm/deals/\(deal.id)", body: body)
        withAnimation(.snappy) {
            if let index = deals.firstIndex(where: { $0.id == deal.id }) {
                // Preserve joined fields the PATCH response may omit.
                var merged = updated
                if merged.contact == nil { merged.contact = deals[index].contact }
                if merged.stage == nil { merged.stage = stage(updated.stageID) }
                deals[index] = merged
            }
        }
    }
}

// MARK: - Tasks

/// Drawer scopes for the tasks browser. All are client-side slices of the
/// loaded page; the search endpoint has no due-date bucket filters.
enum CRMTaskScope: String, CaseIterable, Identifiable {
    case open, dueToday, overdue, upcoming, completed

    var id: String { rawValue }

    var title: String {
        switch self {
        case .open: "Open"
        case .dueToday: "Due today"
        case .overdue: "Overdue"
        case .upcoming: "Upcoming"
        case .completed: "Completed"
        }
    }

    var icon: String {
        switch self {
        case .open: "tray.full.fill"
        case .dueToday: "sun.max.fill"
        case .overdue: "exclamationmark.circle.fill"
        case .upcoming: "calendar"
        case .completed: "checkmark.circle.fill"
        }
    }
}

@MainActor
@Observable
final class CRMTasksStore {
    private(set) var tasks: [CRMTask] = []
    private(set) var types: [CRMTaskType] = []
    private(set) var summary: CRMTasksSummary?
    private(set) var isLoading = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    var query = ""
    var scope: CRMTaskScope = .open

    private var generation = 0

    private func searchBody() -> CRMTaskSearchBody {
        var body = CRMTaskSearchBody()
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        if !trimmed.isEmpty { body.query = trimmed }
        body.sortBy = "due_date"
        body.reverse = true
        return body
    }

    // Section buckets. Overdue floats first, then open (not done, not overdue),
    // then completed/cancelled.
    var overdue: [CRMTask] {
        tasks.filter { $0.isOverdue }
    }

    var open: [CRMTask] {
        tasks.filter { !$0.isDone && !$0.isOverdue }
    }

    var done: [CRMTask] {
        tasks.filter { $0.isDone }
    }

    /// Everything not completed/cancelled, overdue included (the Open scope).
    var notDone: [CRMTask] {
        tasks.filter { !$0.isDone }
    }

    /// Tasks in a drawer scope, sliced client-side from the loaded page.
    func tasks(in scope: CRMTaskScope) -> [CRMTask] {
        let calendar = Calendar.current
        switch scope {
        case .open:
            return notDone
        case .dueToday:
            return notDone.filter { $0.dueDate.map(calendar.isDateInToday) ?? false }
        case .overdue:
            return overdue
        case .upcoming:
            return notDone.filter { task in
                guard let due = task.dueDate else { return false }
                return due > Date() && !calendar.isDateInToday(due)
            }
        case .completed:
            return done
        }
    }

    /// Scope counts for the drawer. Open/overdue/completed prefer the server
    /// summary; due today and upcoming are derived from the loaded page
    /// (limit 200), so they can undercount very large task lists.
    func count(for scope: CRMTaskScope) -> Int {
        switch scope {
        case .open:
            return summary?.openCount ?? notDone.count
        case .overdue:
            return summary?.overdueCount ?? overdue.count
        case .completed:
            if let summary { return (summary.completedCount ?? 0) + (summary.cancelledCount ?? 0) }
            return done.count
        case .dueToday, .upcoming:
            return tasks(in: scope).count
        }
    }

    func load(_ api: APIClient) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            let body = searchBody()
            async let tasksPage: CRMPage<CRMTask> = api.post("crm/tasks/search", body: body, query: ["limit": "200"])
            async let summaryResult: CRMTasksSummary = api.post("crm/tasks/summary", body: body)
            // Task types wrap in {"data":[...]}; tolerate failure (feature seeds on first list).
            async let typesResult: CRMPage<CRMTaskType> = api.get("crm/task-types")

            let page = try await tasksPage
            let summaryValue = try? await summaryResult
            let typesValue = try? await typesResult
            guard gen == generation else { return }

            withAnimation(.snappy) {
                tasks = page.items
                summary = summaryValue
                if let typesValue { types = typesValue.items.sorted { ($0.position ?? 0) < ($1.position ?? 0) } }
            }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
        } catch {
            guard gen == generation else { return }
            if !hasLoaded { errorMessage = error.localizedDescription }
            isLoading = false
        }
    }

    func create(_ api: APIClient, body: CRMTaskCreateBody) async throws {
        let task: CRMTask = try await api.post("crm/tasks", body: body, idempotent: true)
        withAnimation(.snappy) {
            tasks.insert(task, at: 0)
        }
    }

    /// Flip completion. Completed <-> pending; setting completed stamps completed_at server-side.
    func setDone(_ api: APIClient, task: CRMTask, done: Bool) async throws {
        var body = CRMTaskUpdateBody()
        body.status = done ? "completed" : "pending"
        let updated: CRMTask = try await api.patch("crm/tasks/\(task.id)", body: body)
        withAnimation(.snappy) {
            if let index = tasks.firstIndex(where: { $0.id == task.id }) {
                tasks[index] = updated
            }
        }
    }
}

// MARK: - Meetings

/// Drawer scopes for the meetings browser. Today is a client-side slice of the
/// upcoming timeframe (the API only knows upcoming/past).
enum CRMMeetingScope: String, CaseIterable, Identifiable {
    case upcoming, today, past

    var id: String { rawValue }

    var title: String {
        switch self {
        case .upcoming: "Upcoming"
        case .today: "Today"
        case .past: "Past"
        }
    }

    var icon: String {
        switch self {
        case .upcoming: "calendar.badge.clock"
        case .today: "sun.max.fill"
        case .past: "clock.arrow.circlepath"
        }
    }

    /// API `timeframe` query value backing this scope.
    var timeframe: String { self == .past ? "past" : "upcoming" }
}

@MainActor
@Observable
final class CRMMeetingsStore {
    private(set) var meetings: [CRMMeeting] = []
    private(set) var summary: CRMMeetingsSummary?
    private(set) var totalCount: Int?
    private(set) var isLoading = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    var query = ""
    var scope: CRMMeetingScope = .upcoming
    /// "upcoming" | "past" — derived from the scope.
    var timeframe: String { scope.timeframe }

    /// Meetings for the active scope; Today filters the upcoming load.
    var visibleMeetings: [CRMMeeting] {
        scope == .today ? meetings.filter(\.isToday) : meetings
    }

    /// Drawer scope counts from the server summary (past has none).
    func count(for scope: CRMMeetingScope) -> Int? {
        switch scope {
        case .upcoming: summary?.upcoming
        case .today: summary?.today
        case .past: nil
        }
    }

    /// Soonest non-canceled meeting from an upcoming load, for the hero badge.
    var nextUpcoming: CRMMeeting? {
        guard scope != .past else { return nil }
        return meetings.first { $0.status != "canceled" && ($0.scheduledFor ?? .distantPast) >= Date() }
    }

    private var generation = 0

    func load(_ api: APIClient) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            let trimmed = query.trimmingCharacters(in: .whitespaces)
            var mutableParams: [String: String?] = ["limit": "200", "timeframe": timeframe]
            if !trimmed.isEmpty { mutableParams["q"] = trimmed }
            // async let captures concurrently; give it an immutable copy.
            let params = mutableParams

            async let listPage: CRMPage<CRMMeeting> = api.get("meetings", query: params)
            async let summaryResult: CRMMeetingsSummary = api.get("meetings/summary")
            let page = try await listPage
            let summaryValue = try? await summaryResult
            guard gen == generation else { return }

            withAnimation(.snappy) {
                meetings = sortedForTimeframe(page.items)
                totalCount = (page.pagination?.total).map(Int.init)
                summary = summaryValue
            }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
        } catch {
            guard gen == generation else { return }
            if !hasLoaded { errorMessage = error.localizedDescription }
            isLoading = false
        }
    }

    // Upcoming sorts soonest-first; past shows most recent first.
    private func sortedForTimeframe(_ items: [CRMMeeting]) -> [CRMMeeting] {
        items.sorted { lhs, rhs in
            let l = lhs.scheduledFor ?? .distantPast
            let r = rhs.scheduledFor ?? .distantPast
            return timeframe == "upcoming" ? l < r : l > r
        }
    }

    func cancel(_ api: APIClient, meeting: CRMMeeting) async throws {
        let _: CRMDeletedResponse = try await api.delete("meetings/\(meeting.id)")
        withAnimation(.snappy) {
            meetings.removeAll { $0.id == meeting.id }
            if let total = totalCount, total > 0 { totalCount = total - 1 }
        }
    }
}

// MARK: - Templates

@MainActor
@Observable
final class TemplatesStore {
    private(set) var templates: [EmailTemplate] = []
    private(set) var isLoading = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    var query = ""

    private var generation = 0

    func load(_ api: APIClient) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            var params: [String: String?] = [:]
            let trimmed = query.trimmingCharacters(in: .whitespaces)
            if !trimmed.isEmpty { params["q"] = trimmed }
            let page: CRMPage<EmailTemplate> = try await api.get("templates", query: params)
            guard gen == generation else { return }
            withAnimation(.snappy) {
                templates = page.items.sorted { ($0.position ?? 0) < ($1.position ?? 0) }
            }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
        } catch {
            guard gen == generation else { return }
            if !hasLoaded { errorMessage = error.localizedDescription }
            isLoading = false
        }
    }

    func duplicate(_ api: APIClient, template: EmailTemplate) async throws {
        let copy: EmailTemplate = try await api.post("templates/\(template.id)/duplicate")
        withAnimation(.snappy) {
            templates.append(copy)
            templates.sort { ($0.position ?? 0) < ($1.position ?? 0) }
        }
    }
}
