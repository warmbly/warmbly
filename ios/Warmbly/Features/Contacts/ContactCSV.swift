import Foundation

/// Where a CSV column maps to. Categories/campaigns are applied uniformly on the
/// organize step (not per-column), so the per-column targets stay simple; any
/// non-standard column can land in the contact's custom fields.
enum ContactColumnTarget: String, CaseIterable, Hashable, Sendable {
    case ignore, email, firstName, lastName, company, phone, custom

    var label: String {
        switch self {
        case .ignore: "Ignore"
        case .email: "Email"
        case .firstName: "First name"
        case .lastName: "Last name"
        case .company: "Company"
        case .phone: "Phone"
        case .custom: "Custom field"
        }
    }
}

/// A parsed delimited file plus a best-guess column mapping.
struct ContactCSV: Sendable {
    var columns: [String]
    var rows: [[String]]
    var hasHeader: Bool

    /// Parses CSV/TSV text, honouring quoted fields, escaped quotes, and CRLF.
    static func parse(_ text: String) -> ContactCSV {
        let delimiter: Character = text.prefix(2000).contains("\t") && !text.prefix(2000).contains(",") ? "\t" : ","
        var records: [[String]] = []
        var field = ""
        var record: [String] = []
        var inQuotes = false
        let scalars = Array(text)
        var i = 0
        while i < scalars.count {
            let c = scalars[i]
            if inQuotes {
                if c == "\"" {
                    if i + 1 < scalars.count, scalars[i + 1] == "\"" { field.append("\""); i += 1 }
                    else { inQuotes = false }
                } else {
                    field.append(c)
                }
            } else {
                switch c {
                case "\"": inQuotes = true
                case delimiter: record.append(field); field = ""
                case "\n":
                    record.append(field); field = ""
                    records.append(record); record = []
                case "\r": break
                default: field.append(c)
                }
            }
            i += 1
        }
        if !field.isEmpty || !record.isEmpty { record.append(field); records.append(record) }
        // Drop fully-empty trailing records.
        records = records.filter { !($0.count == 1 && $0[0].trimmingCharacters(in: .whitespaces).isEmpty) }
        guard !records.isEmpty else { return ContactCSV(columns: [], rows: [], hasHeader: true) }

        let width = records.map(\.count).max() ?? 0
        let normalized = records.map { row -> [String] in
            var r = row.map { $0.trimmingCharacters(in: .whitespaces) }
            while r.count < width { r.append("") }
            return r
        }
        let header = normalized[0]
        return ContactCSV(columns: header, rows: Array(normalized.dropFirst()), hasHeader: true)
    }

    /// Header names shown for each column (real header, or "Column N").
    func displayColumns() -> [String] {
        hasHeader ? columns.map { $0.isEmpty ? "Untitled" : $0 } : (0 ..< columns.count).map { "Column \($0 + 1)" }
    }

    /// The rows to actually import (all rows if the header line is data).
    var dataRows: [[String]] { hasHeader ? rows : ([columns] + rows) }

    /// Best-guess mapping from column header names.
    func guessMapping() -> [ContactColumnTarget] {
        displayColumns().map { name in
            let n = name.lowercased()
            if n.contains("email") || n.contains("e-mail") { return .email }
            if n.contains("first") { return .firstName }
            if n.contains("last") || n.contains("surname") { return .lastName }
            if n.contains("company") || n.contains("organization") || n.contains("organisation") { return .company }
            if n.contains("phone") || n.contains("mobile") || n.contains("tel") { return .phone }
            if n == "name" || n == "full name" { return .firstName }
            return .custom
        }
    }
}

enum ContactCSVBuild {
    private static func validEmail(_ raw: String) -> Bool {
        let t = raw.trimmingCharacters(in: .whitespaces)
        return t.contains("@") && t.contains(".") && !t.hasSuffix("@")
    }

    /// Rows that carry a valid email (the count we can actually import).
    static func importableCount(_ csv: ContactCSV, mapping: [ContactColumnTarget]) -> Int {
        guard let emailIdx = mapping.firstIndex(of: .email) else { return 0 }
        return csv.dataRows.filter { emailIdx < $0.count && validEmail($0[emailIdx]) }.count
    }

    /// Build create bodies from the mapped rows, applying uniform categories +
    /// campaigns. Rows without a valid email are skipped.
    static func bodies(
        _ csv: ContactCSV,
        mapping: [ContactColumnTarget],
        headers: [String],
        categories: [String]?,
        campaigns: [String]?
    ) -> [ContactCreateBody] {
        guard let emailIdx = mapping.firstIndex(of: .email) else { return [] }
        var out: [ContactCreateBody] = []
        for row in csv.dataRows {
            guard emailIdx < row.count, validEmail(row[emailIdx]) else { continue }
            var body = ContactCreateBody(email: row[emailIdx].trimmingCharacters(in: .whitespaces).lowercased())
            var custom: [String: String] = [:]
            for (idx, target) in mapping.enumerated() where idx < row.count {
                let value = row[idx].trimmingCharacters(in: .whitespaces)
                guard !value.isEmpty else { continue }
                switch target {
                case .firstName: body.firstName = value
                case .lastName: body.lastName = value
                case .company: body.company = value
                case .phone: body.phone = value
                case .custom:
                    let key = idx < headers.count ? headers[idx] : "field_\(idx + 1)"
                    if !key.isEmpty { custom[key] = value }
                case .ignore, .email: break
                }
            }
            if !custom.isEmpty { body.customFields = custom }
            body.categories = categories
            body.campaigns = campaigns
            out.append(body)
        }
        return out
    }
}
