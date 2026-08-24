import Foundation

/// SKILL.md frontmatter parsing and provenance stamping. Mirrors
/// `parseSummary` / `parseFlatYAML` in `cli/internal/registry/registry.go` and
/// `mergeFrontmatter` in `cli/cmd/skills-registry/provenance.go`: handles flat
/// `key: value` lines plus YAML folded/literal block scalars (`>`, `>-`, `|`,
/// `|-`), and tolerates keys it does not know. Keep in sync with the Go
/// implementations.
public enum Frontmatter {
    private static let blockScalarMarkers: Set<String> = [
        ">", ">-", ">+", "|", "|-", "|+",
    ]

    /// One frontmatter key to merge. A slice of these rather than a dictionary
    /// so the written order is deterministic.
    public struct Key: Sendable, Equatable {
        public var name: String
        public var value: String

        public init(name: String, value: String) {
            self.name = name
            self.value = value
        }
    }

    /// Key names the untrusted-import stamp writes. Two extra frontmatter keys
    /// record where an imported copy came from, so provenance lives in the file
    /// rather than only in the registry commit message.
    public static let categoryKey = "category"
    public static let sourceURLKey = "source_url"

    /// Extract the display name + description for a registry listing row.
    /// Falls back to `slug` for the name and the first paragraph for the
    /// description. Whitespace is collapsed; description capped at 300 chars.
    public static func parseSummary(_ text: String, slug: String) -> (name: String, description: String) {
        var name = slug
        var description = ""

        if text.hasPrefix("---") {
            let lines = text.components(separatedBy: "\n")
            var end = -1
            var i = 1
            while i < lines.count {
                if lines[i].trimmingCharacters(in: .whitespaces) == "---" { end = i; break }
                i += 1
            }
            if end > 0 {
                let meta = parseFlatYAML(Array(lines[1..<end]))
                if let v = meta["name"], !v.isEmpty { name = v }
                if let v = meta["description"], !v.isEmpty { description = v }
                if description.isEmpty && end + 1 < lines.count {
                    description = firstParagraph(lines[(end + 1)...].joined(separator: "\n"))
                }
            }
        } else {
            description = firstParagraph(text)
        }

        description = description.split(whereSeparator: { $0 == " " || $0 == "\t" || $0 == "\n" || $0 == "\r" })
            .joined(separator: " ")
        if description.count > 300 {
            description = String(description.prefix(300))
        }
        if description.isEmpty {
            description = "Skill: \(name)"
        }
        return (name, description)
    }

    /// Return everything after the closing `---` (the markdown body), or the
    /// whole text when there's no frontmatter block.
    public static func body(_ text: String) -> String {
        guard text.hasPrefix("---") else { return text }
        let lines = text.components(separatedBy: "\n")
        var end = -1
        var i = 1
        while i < lines.count {
            if lines[i].trimmingCharacters(in: .whitespaces) == "---" { end = i; break }
            i += 1
        }
        guard end > 0, end + 1 <= lines.count else { return text }
        var rest = lines[(end + 1)...].joined(separator: "\n")
        while rest.hasPrefix("\n") { rest.removeFirst() }
        return rest
    }

    // MARK: - provenance stamping

    /// Merge `keys` into `text`'s frontmatter, returning the rewritten document
    /// or `nil` when nothing changed. Swift mirror of Go `mergeFrontmatter`.
    ///
    /// The document is edited line by line rather than parsed and
    /// re-serialized, so every unrelated line — key order, comments, block
    /// scalars, quoting style — survives byte-for-byte. A key already present
    /// keeps its own value unless that value is empty; a missing key is
    /// appended just before the closing `---`. A document with no frontmatter
    /// gains a block holding only these keys. A document whose block is never
    /// closed is left alone: guessing where its metadata ends would risk
    /// rewriting the body.
    public static func merging(_ text: String, keys: [Key]) -> String? {
        let wanted = keys.filter { !$0.name.isEmpty }
        guard !wanted.isEmpty else { return nil }
        guard text.hasPrefix("---") else { return prepending(text, keys: wanted) }
        var lines = text.components(separatedBy: "\n")
        guard var end = closingFenceIndex(lines) else { return nil }
        var changed = false
        for k in wanted {
            let line = "\(k.name): \(yamlScalar(k.value))"
            if let at = topLevelKeyLine(Array(lines[1..<end]), key: k.name) {
                if !frontmatterValue(lines[1 + at]).isEmpty { continue }
                lines[1 + at] = line
                changed = true
                continue
            }
            lines.insert(line, at: end)
            end += 1
            changed = true
        }
        return changed ? lines.joined(separator: "\n") : nil
    }

    /// Give a document with no frontmatter one carrying just `keys`.
    private static func prepending(_ text: String, keys: [Key]) -> String {
        let block = keys.map { "\($0.name): \(yamlScalar($0.value))" }.joined(separator: "\n")
        return "---\n\(block)\n---\n\(text)"
    }

    /// Index of the frontmatter block's closing `---`, or `nil` when the block
    /// is never closed.
    private static func closingFenceIndex(_ lines: [String]) -> Int? {
        var i = 1
        while i < lines.count {
            if lines[i].trimmingCharacters(in: .whitespaces) == "---" { return i }
            i += 1
        }
        return nil
    }

    /// Find a top-level `key:` line in a frontmatter block. Indented lines are
    /// continuations (a block scalar's text, a nested mapping), so they never
    /// match.
    private static func topLevelKeyLine(_ block: [String], key: String) -> Int? {
        for (i, raw) in block.enumerated() {
            if raw.isEmpty || raw.hasPrefix(" ") || raw.hasPrefix("\t") { continue }
            guard let colon = raw.firstIndex(of: ":") else { continue }
            if String(raw[..<colon]).trimmingCharacters(in: .whitespaces) == key { return i }
        }
        return nil
    }

    /// A key line's value, with surrounding quotes stripped so `category: ""`
    /// counts as empty and gets filled.
    private static func frontmatterValue(_ line: String) -> String {
        guard let colon = line.firstIndex(of: ":") else { return "" }
        let raw = String(line[line.index(after: colon)...]).trimmingCharacters(in: .whitespaces)
        return trimQuotes(raw).trimmingCharacters(in: .whitespaces)
    }

    /// Characters YAML gives special meaning at the start of a plain scalar.
    private static let yamlIndicators = Set("-?:,[]{}#&*!|>'\"%@`")

    /// Render a value as a YAML scalar, quoting only when a plain one would be
    /// ambiguous. A URL stays unquoted, because a colon is only special when
    /// whitespace follows it. A category comes from a third-party index, so a
    /// value carrying a newline, a quote, or a leading indicator is quoted
    /// rather than trusted to be well behaved.
    static func yamlScalar(_ v: String) -> String {
        if v.isEmpty { return "\"\"" }
        let needsQuoting = v.trimmingCharacters(in: .whitespacesAndNewlines) != v
            || v.contains(where: { "\n\r\"'#".contains($0) })
            || v.contains(": ")
            || v.hasSuffix(":")
            || yamlIndicators.contains(v.first!)
        guard needsQuoting else { return v }
        var escaped = ""
        for c in v {
            switch c {
            case "\\": escaped += "\\\\"
            case "\"": escaped += "\\\""
            case "\n": escaped += "\\n"
            case "\r": escaped += "\\r"
            case "\t": escaped += "\\t"
            default: escaped.append(c)
            }
        }
        return "\"\(escaped)\""
    }

    // MARK: - flat YAML

    static func parseFlatYAML(_ body: [String]) -> [String: String] {
        var out: [String: String] = [:]
        var i = 0
        while i < body.count {
            let raw = body[i]
            let stripped = raw.trimmingCharacters(in: .whitespaces)
            if stripped.isEmpty || stripped.hasPrefix("#") || !raw.contains(":") {
                i += 1
                continue
            }
            guard let colon = raw.firstIndex(of: ":") else { i += 1; continue }
            let key = String(raw[..<colon]).trimmingCharacters(in: .whitespaces)
            let val = String(raw[raw.index(after: colon)...]).trimmingCharacters(in: .whitespaces)

            let head = val.split(separator: " ").first.map(String.init) ?? val
            if blockScalarMarkers.contains(head) {
                let folded = head.hasPrefix(">")
                let (block, nextI) = collectBlockLines(body, i + 1)
                i = nextI
                if folded {
                    out[key] = foldBlockScalar(block)
                } else {
                    out[key] = block.joined(separator: "\n").replacingOccurrences(
                        of: "\n+$", with: "", options: .regularExpression)
                }
                continue
            }

            var value = trimQuotes(val)
            if !value.isEmpty {
                let (cont, nextI) = collectPlainContinuationLines(body, i + 1)
                if !cont.isEmpty {
                    value = ([value] + cont).joined(separator: " ")
                    i = nextI
                } else {
                    i += 1
                }
            } else {
                i += 1
            }
            out[key] = value
        }
        return out
    }

    private static func collectPlainContinuationLines(_ body: [String], _ start: Int) -> ([String], Int) {
        var cont: [String] = []
        var i = start
        while i < body.count {
            let peek = body[i]
            let stripped = peek.trimmingCharacters(in: .whitespaces)
            if stripped.isEmpty || stripped.hasPrefix("#") { break }
            if !peek.hasPrefix(" ") && !peek.hasPrefix("\t") { break }
            cont.append(stripped)
            i += 1
        }
        return (cont, i)
    }

    private static func collectBlockLines(_ body: [String], _ start: Int) -> ([String], Int) {
        var block: [String] = []
        var i = start
        while i < body.count {
            let peek = body[i]
            if peek.trimmingCharacters(in: .whitespaces).isEmpty {
                block.append("")
                i += 1
                continue
            }
            if !peek.hasPrefix(" ") && !peek.hasPrefix("\t") { break }
            block.append(peek.trimmingCharacters(in: .whitespaces))
            i += 1
        }
        return (block, i)
    }

    private static func foldBlockScalar(_ block: [String]) -> String {
        var paragraphs: [[String]] = []
        var current: [String] = []
        for ln in block {
            if ln.isEmpty {
                if !current.isEmpty { paragraphs.append(current); current = [] }
                continue
            }
            current.append(ln)
        }
        if !current.isEmpty { paragraphs.append(current) }
        return paragraphs.map { $0.joined(separator: " ") }.joined(separator: "\n\n")
    }

    private static func trimQuotes(_ s: String) -> String {
        var r = s
        if r.count >= 2, let f = r.first, let l = r.last, f == l, f == "'" || f == "\"" {
            r.removeFirst()
            r.removeLast()
        }
        return r
    }

    static func firstParagraph(_ text: String) -> String {
        for block in text.components(separatedBy: "\n\n") {
            let cleaned = block.trimmingCharacters(in: .whitespacesAndNewlines)
            if cleaned.isEmpty || cleaned.hasPrefix("#") { continue }
            return cleaned
        }
        return text.trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
