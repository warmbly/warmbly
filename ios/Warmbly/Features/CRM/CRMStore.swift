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
            async let dealsPage: CRMPage<CRMDeal> = api.post("crm/deals/search", body: body, query: ["limit": "200"])
            async let summaryResult: CRMDealsSummary = api.post("crm/deals/summary", body: body)
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
    /// "upcoming" | "past".
    var timeframe = "upcoming"

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
