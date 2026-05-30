import Foundation

/// SKILL.md frontmatter parsing. Mirrors `parseSummary` / `parseFlatYAML`
/// in `cli/internal/registry/registry.go` (and `frontmatter.py`): handles
/// flat `key: value` lines plus YAML folded/literal block scalars
/// (`>`, `>-`, `|`, `|-`). Keep in sync with the Go/Python implementations.
public enum Frontmatter {
    private static let blockScalarMarkers: Set<String> = [
        ">", ">-", ">+", "|", "|-", "|+",
    ]

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
