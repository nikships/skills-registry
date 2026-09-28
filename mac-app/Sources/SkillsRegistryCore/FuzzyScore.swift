import Foundation

/// fzf V1-style fuzzy scorer shared by the app and CLI. The constants are
/// duplicated by design; their cross-language corpus tests pin the contract.
enum FuzzyConst {
    static let baseMatchScore = 16
    static let boundaryBonus = 8
    static let camelBonus = 7
    static let consecutiveBonus = 5
    static let caseBonus = 1
    static let gapPenalty = 2
    static let searchTopN = 10
    /// (field, weight) — name is the most precise label; slug/desc tiebreak.
    static let fieldWeights: [(field: SkillField, weight: Int)] = [
        (.name, 2), (.slug, 1), (.description, 1),
    ]
}

enum SkillField { case name, slug, description }

private func isWordBoundaryChar(_ c: Character) -> Bool {
    switch c {
    case " ", "\t", "\n", "_", "-", "/", ".", "\\", ":": return true
    default: return false
    }
}

/// Locate the tightest right-anchored alignment of `qLower` in `tLower`.
/// Returns nil if any query char doesn't appear in order.
private func findAlignment(_ qLower: [Character], _ tLower: [Character]) -> [Int]? {
    var qi = 0
    var endIdx = -1
    for ti in 0..<tLower.count {
        if tLower[ti] == qLower[qi] {
            qi += 1
            if qi == qLower.count {
                endIdx = ti
                break
            }
        }
    }
    if endIdx < 0 { return nil }

    var matches = [Int](repeating: 0, count: qLower.count)
    qi = qLower.count - 1
    var ti = endIdx
    while ti >= 0 && qi >= 0 {
        if tLower[ti] == qLower[qi] {
            matches[qi] = ti
            qi -= 1
        }
        ti -= 1
    }
    if qi >= 0 { return nil }
    return matches
}

private func scoreCharMatch(
    qPos: Int, tPos: Int, matches: [Int],
    queryRunes: [Character], textRunes: [Character], tLower: [Character]
) -> Int {
    var score = FuzzyConst.baseMatchScore
    if tPos == 0 || isWordBoundaryChar(tLower[tPos - 1]) {
        score += FuzzyConst.boundaryBonus
    } else if tPos > 0 && textRunes[tPos].isUppercase && textRunes[tPos - 1].isLowercase {
        score += FuzzyConst.camelBonus
    }
    if qPos > 0 && matches[qPos] == matches[qPos - 1] + 1 {
        score += FuzzyConst.consecutiveBonus
    }
    if textRunes[tPos] == queryRunes[qPos] {
        score += FuzzyConst.caseBonus
    }
    return score
}

/// Score a (query, text) pair. Returns 0 when no alignment exists.
///
/// Both strings are normalized to NFC before scoring so that precomposed
/// and decomposed spellings of the same text (e.g. "é" vs "e"+U+0301)
/// score identically. The Go scorer applies the same normalization;
/// the cross-language corpus pins this.
public func fuzzyScore(_ query: String, _ text: String) -> Int {
    if query.isEmpty || text.isEmpty { return 0 }
    let query = query.precomposedStringWithCanonicalMapping
    let text = text.precomposedStringWithCanonicalMapping
    let queryRunes = Array(query)
    let textRunes = Array(text)
    if queryRunes.count > textRunes.count { return 0 }
    let qLower = Array(query.lowercased())
    let tLower = Array(text.lowercased())
    // Lowercasing can change length for exotic scripts. Bail out so the
    // index math below stays sound. NFC Latin accents in the corpus do not
    // change length.
    guard qLower.count == queryRunes.count, tLower.count == textRunes.count else { return 0 }
    guard let matches = findAlignment(qLower, tLower) else { return 0 }
    var score = 0
    for qPos in 0..<matches.count {
        score += scoreCharMatch(
            qPos: qPos, tPos: matches[qPos], matches: matches,
            queryRunes: queryRunes, textRunes: textRunes, tLower: tLower
        )
    }
    let span = matches[matches.count - 1] - matches[0] + 1
    score -= (span - qLower.count) * FuzzyConst.gapPenalty
    return max(0, score)
}

/// Sum per-field fuzzy scores under the canonical field weights.
func scoreSkill(_ query: String, _ s: SkillSummary) -> Int {
    let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
    if q.isEmpty { return 0 }
    var score = 0
    for fw in FuzzyConst.fieldWeights {
        let field: String
        switch fw.field {
        case .name: field = s.name
        case .slug: field = s.slug
        case .description: field = s.description
        }
        score += fuzzyScore(q, field) * fw.weight
    }
    return score
}

/// Summaries ranked by `scoreSkill`, best first. An empty / whitespace-only
/// query returns []. Ties break on slug ascending, identical to the Go sort.
///
/// The default `limit` (nil) keeps the cross-language top-N contract pinned
/// (the CLI `search` caps at the same N); callers that must show every match
/// — like Browse, the analog of the CLI `list` TUI — pass a larger limit.
public func scoreAndSort(
    _ summaries: [SkillSummary], query: String, limit: Int? = nil
) -> [SkillSummary] {
    let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
    if q.isEmpty { return [] }
    var scored: [(score: Int, summary: SkillSummary)] = []
    for s in summaries {
        let sc = scoreSkill(q, s)
        if sc > 0 { scored.append((sc, s)) }
    }
    scored.sort { a, b in
        if a.score != b.score { return a.score > b.score }
        return a.summary.slug < b.summary.slug
    }
    let effective = limit ?? FuzzyConst.searchTopN
    let capped = min(max(0, effective), scored.count)
    return Array(scored.prefix(capped).map { $0.summary })
}
