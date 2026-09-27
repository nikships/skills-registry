import Foundation

/// An offline heuristic scan of a SKILL.md for content hostile to the agent
/// that will load it. Swift mirror of Go `cli/internal/skillscan`, kept in
/// lockstep: the same 11 line-scoped rules in the same 3 categories, the same
/// input cap, and the same output bounds.
///
/// A skill file is prose an agent reads as instructions, so a public skill can
/// carry a prompt-injection payload, a credential-exfiltration recipe, or a
/// pipe-to-shell installer with nothing about the file signalling danger. This
/// looks for the three shapes with regular expressions and reports what it
/// matched.
///
/// It is a warning layer, not a scanner in the antivirus sense. There is no
/// model, no network call, and no sandbox: obfuscation, a payload split across
/// lines, and anything expressed indirectly all pass. A clean result means
/// "none of these patterns matched", never "this skill is safe". Callers must
/// present findings as a prompt to read the source, and must keep the decision
/// with the user.
///
/// The rules are tuned to keep false positives low enough that a hit is worth
/// reading: patterns that need a sink (a shell pipe, an HTTP POST) only fire
/// when the sink is on the same line as the secret, and documentation-shaped
/// lines such as `curl -H "Authorization: Bearer $API_KEY" https://api…` do
/// not fire at all.
public enum SkillScan {
    /// Caps how much of a file is scanned. A SKILL.md is prose; past this the
    /// file is data, and scanning it would only slow an import.
    public static let maxScanBytes = 1 << 20

    /// Bounds the quoted line in a finding so one long line cannot flood the
    /// UI.
    static let maxExcerptRunes = 140

    /// Caps the reported findings. The point is to make the user look at the
    /// file, and a dozen hits already does that.
    static let maxFindings = 24

    /// Caps how many times one rule reports, so a file that repeats the same
    /// pattern does not crowd out the other categories.
    static let maxPerRule = 3

    /// Report every heuristic hit in `text`. The whole file is scanned,
    /// frontmatter included: a `description` an agent reads at load time is as
    /// good a carrier as the body.
    ///
    /// Findings are returned in file order. Results are bounded (`maxPerRule`
    /// per rule, `maxFindings` overall) so a hostile file cannot produce
    /// unbounded output.
    public static func scan(_ text: String) -> [SkillFinding] {
        // Byte-cap before splitting, exactly like Go's `text[:MaxScanBytes]`:
        // content past the cap is never scanned. A trailing partial rune
        // decodes lossy rather than invalid, which can only drop a match on
        // the cut line, never invent one.
        let capped = String(decoding: text.utf8.prefix(maxScanBytes), as: UTF8.self)
        var out: [SkillFinding] = []
        var perRule: [String: Int] = [:]
        // `omittingEmptySubsequences: false` so a trailing newline keeps its
        // empty tail, matching Go's `strings.Split`.
        let lines = capped.split(separator: "\n", omittingEmptySubsequences: false)
        for (index, substring) in lines.enumerated() {
            if out.count >= maxFindings { break }
            let line = String(substring)
            for rule in SkillScanRule.all {
                guard (perRule[rule.name] ?? 0) < maxPerRule, rule.fires(line) else { continue }
                perRule[rule.name, default: 0] += 1
                out.append(SkillFinding(
                    category: rule.category, rule: rule.name,
                    line: index + 1, excerpt: excerpt(line)))
                if out.count >= maxFindings { break }
            }
        }
        return out
    }

    /// Scan one file, reading at most `maxScanBytes`.
    public static func scanFile(_ path: String) throws -> [SkillFinding] {
        let handle = try FileHandle(forReadingFrom: URL(fileURLWithPath: path))
        defer { try? handle.close() }
        let data = (try handle.read(upToCount: maxScanBytes)) ?? Data()
        return scan(String(decoding: data, as: UTF8.self))
    }

    /// Scan the SKILL.md inside a skill folder. A folder with no SKILL.md
    /// yields no findings and no error: discovery already decides what is a
    /// skill, and this does not duplicate that judgement.
    public static func scanSkill(folder: String, mainFileName: String = Scan.mainFileName) throws -> [SkillFinding] {
        let path = (folder as NSString).appendingPathComponent(mainFileName)
        guard FileManager.default.fileExists(atPath: path) else { return [] }
        return try scanFile(path)
    }

    /// The distinct categories present in `findings`, in the order they first
    /// appear. Used to summarize a hit without listing every line.
    public static func categories(in findings: [SkillFinding]) -> [SkillScanCategory] {
        var seen = Set<SkillScanCategory>()
        var out: [SkillScanCategory] = []
        for finding in findings where seen.insert(finding.category).inserted {
            out.append(finding.category)
        }
        return out
    }

    /// Render the categories present as a comma-separated phrase, for a
    /// one-line warning. Empty for no findings.
    public static func summary(of findings: [SkillFinding]) -> String {
        categories(in: findings).map(\.label).joined(separator: ", ")
    }

    /// Collapse whitespace and cap length so a finding is one readable line
    /// regardless of the source formatting. The cap counts Unicode scalars,
    /// matching Go's rune count.
    static func excerpt(_ line: String) -> String {
        let collapsed = line.split(whereSeparator: { $0.isWhitespace || $0.isNewline })
            .joined(separator: " ")
        let scalars = collapsed.unicodeScalars
        guard scalars.count > maxExcerptRunes else { return collapsed }
        return String(String.UnicodeScalarView(scalars.prefix(maxExcerptRunes - 1))) + "…"
    }
}

/// Groups findings by the kind of harm the pattern points at.
public enum SkillScanCategory: String, Sendable, Hashable, Codable {
    /// Text aimed at the reading agent rather than the user: instruction
    /// overrides, hidden-from-the-user directives, and guardrail-disabling
    /// role play.
    case promptInjection = "prompt_injection"
    /// Reading a secret (an SSH key, a cloud credential file, the
    /// environment) and shipping it somewhere.
    case credentialExfiltration = "credential_exfiltration"
    /// Downloading code and executing it in one step, `curl … | sh` and its
    /// relatives.
    case remoteExecution = "remote_execution"

    /// Render a category for a human.
    public var label: String {
        switch self {
        case .promptInjection: return "prompt injection"
        case .credentialExfiltration: return "credential exfiltration"
        case .remoteExecution: return "remote code execution"
        }
    }
}

/// One matched line.
public struct SkillFinding: Sendable, Hashable, Codable {
    public var category: SkillScanCategory
    /// The stable identifier of the pattern that matched, so a consumer can
    /// special-case one heuristic without parsing prose.
    public var rule: String
    /// The 1-based line number in the scanned file.
    public var line: Int
    /// The matched line, whitespace-collapsed and length-capped.
    public var excerpt: String

    public init(category: SkillScanCategory, rule: String, line: Int, excerpt: String) {
        self.category = category
        self.rule = rule
        self.line = line
        self.excerpt = excerpt
    }
}

extension SkillFinding: CustomStringConvertible {
    /// Render one finding as a single line.
    public var description: String {
        "\(category.label) · line \(line) · \(rule) · \(excerpt.debugDescription)"
    }
}

/// One heuristic. A rule fires when `match` hits a line, with (when set) also
/// hits it, and `unless` (when set) does not.
///
/// The patterns are transcribed verbatim from the Go ruleset
/// (`cli/internal/skillscan`), which is the contract: any edit here must land
/// there too, and the ported rule-table tests pin both.
struct SkillScanRule {
    let name: String
    let category: SkillScanCategory
    let match: NSRegularExpression
    let with: NSRegularExpression?
    let unless: NSRegularExpression?

    init(_ name: String, _ category: SkillScanCategory,
         _ match: String, with: String? = nil, unless: String? = nil) {
        self.name = name
        self.category = category
        self.match = Self.compile(match)
        self.with = with.map(Self.compile)
        self.unless = unless.map(Self.compile)
    }

    /// Whether the rule matches one line.
    func fires(_ line: String) -> Bool {
        guard Self.hits(match, line) else { return false }
        if let with, !Self.hits(with, line) { return false }
        if let unless, Self.hits(unless, line) { return false }
        return true
    }

    private static func hits(_ regex: NSRegularExpression, _ line: String) -> Bool {
        regex.firstMatch(in: line, range: NSRange(line.startIndex..., in: line)) != nil
    }

    private static func compile(_ pattern: String) -> NSRegularExpression {
        // The patterns are constants pinned by the rule-table tests; a typo
        // must fail loudly at startup rather than ship a silent rule.
        // swiftlint:disable:next force_try
        try! NSRegularExpression(pattern: pattern)
    }

    /// A destination a secret could leave through: a pipe into a network
    /// client, an HTTP body upload, a raw socket, or a webhook.
    private static let exfilSink = #"(?i)(\|\s*(curl|wget|nc|ncat|netcat|telnet|mail|sendmail|openssl)\b|\bcurl\b[^|\n]*?(--data|--data-binary|--data-raw|-d\s|-F\s|--form|-T\s|--upload-file|-X\s*(POST|PUT))|\bwget\b[^|\n]*--post-(data|file)|/dev/tcp/|requests\.(post|put)\(|urllib\.request\.urlopen\(|\bwebhook\b|Invoke-(RestMethod|WebRequest)\b[^\n]*-Method\s*Post)"#

    /// A file or command that yields a long-lived credential. Generic
    /// `API_KEY`-style names are deliberately absent: a skill that documents
    /// its own API key is normal, and the shape that matters is reading
    /// someone's stored credentials.
    private static let secretStore = #"(?i)(\.ssh/(id_[a-z0-9_]+|authorized_keys)|\bid_(rsa|dsa|ecdsa|ed25519)\b|\.aws/credentials|\.config/gh/hosts\.ya?ml|\bgh\s+auth\s+token\b|\.netrc\b|\.npmrc\b|\.pypirc\b|\.docker/config\.json|\.kube/config\b|security\s+find-(generic|internet)-password|\.env(\.[a-z0-9]+)?\b|BEGIN\s+(RSA\s+|OPENSSH\s+|EC\s+)?PRIVATE\s+KEY)"#

    /// A bulk read of the process environment.
    private static let envDump = #"(?i)(\bprintenv\b|\benv\s*\||\bset\s*\|\s*(grep|curl)|os\.environ\b|process\.env\b|Get-ChildItem\s+Env:|\bdotenv\b)"#

    /// The full heuristic set. Every pattern is line-scoped: a payload split
    /// across lines is out of reach, which is stated in the header comment and
    /// in the user-facing docs.
    static let all: [SkillScanRule] = [
        SkillScanRule("ignore-previous-instructions", .promptInjection,
                      #"(?i)\b(ignore|disregard|forget|override|discard)\b[^.!?\n]{0,48}?\b(all\s+)?(previous|prior|preceding|earlier|above|foregoing|the\s+above)\b[^.!?\n]{0,48}?\b(instruction|instructions|prompt|prompts|rule|rules|direction|directions|guideline|guidelines|context|message|messages)\b"#),
        SkillScanRule("override-system-prompt", .promptInjection,
                      #"(?i)\b(ignore|disregard|override|bypass|replace|forget)\b[^.!?\n]{0,32}?\b(system|developer|operator)\s+(prompt|message|instructions|rules)\b"#),
        SkillScanRule("hide-from-user", .promptInjection,
                      #"(?i)(\b(do\s+not|don'?t|never)\b[^.!?\n]{0,32}?\b(tell|inform|notify|show|mention|reveal|disclose|report|display)\b[^.!?\n]{0,24}?\b(the\s+)?(user|human|operator|owner)\b|\bwithout\s+(telling|informing|notifying|asking|alerting)\s+(the\s+)?(user|human|operator)\b|\b(silently|covertly|secretly)\b[^.!?\n]{0,24}?\b(run|execute|send|upload|exfiltrate|install|delete)\b)"#),
        SkillScanRule("disable-guardrails", .promptInjection,
                      #"(?i)(\b(disable|turn\s+off|switch\s+off|bypass|circumvent|ignore)\b[^.!?\n]{0,32}?\b(safety|safeguard|safeguards|guardrail|guardrails|restriction|restrictions|content\s+polic\w+|filter|filters|approval|permission\s+prompt)\b|\byou\s+are\s+(now\s+)?(in\s+)?(dan\b|developer\s+mode|god\s+mode|jailbroken|unrestricted|unfiltered)|\b(enter|enable|activate)\s+(dan|developer|god|unrestricted)\s+mode\b)"#),
        SkillScanRule("secret-file-exfiltration", .credentialExfiltration,
                      secretStore, with: exfilSink),
        SkillScanRule("environment-exfiltration", .credentialExfiltration,
                      envDump, with: exfilSink),
        SkillScanRule("exfiltration-instruction", .credentialExfiltration,
                      #"(?i)(\bexfiltrat\w*\b|\b(send|upload|post|forward|email|transmit|leak|copy)\b[^.!?\n]{0,48}?\b((the\s+|their\s+|his\s+|her\s+)?(user'?s?|users'?)\s+(\w+\s+){0,2}(api[\s_-]?keys?|secrets?|tokens?|passwords?|credentials?|private\s+keys?)|contents\s+of\s+(the\s+)?[~/.\w-]*\.env\b|(the\s+)?\.env\s+file\b|[~/.\w-]*\.ssh/id_\w+))"#),
        SkillScanRule("pipe-to-shell", .remoteExecution,
                      #"(?i)\b(curl|wget|fetch|iwr|invoke-webrequest)\b[^|\n]*\|\s*(sudo\s+)?(env\s+\S+\s+)?(ba|z|k|da|c|fi)?sh\b"#),
        SkillScanRule("pipe-to-interpreter", .remoteExecution,
                      #"(?i)\b(curl|wget|fetch)\b[^|\n]*\|\s*(python3?|perl|ruby|node|osascript|pwsh|powershell)\s*(-\s|-$|$)"#),
        SkillScanRule("eval-downloaded-code", .remoteExecution,
                      #"(?i)((eval|source|exec)\s*\(?\s*"?\$?\(\s*(curl|wget)\b|\b(ba|z|k)?sh\s+(-[a-z]+\s+)*<\(\s*(curl|wget)\b|\bpython3?\s+-c\s+["'][^"'\n]*urlopen\b)"#),
        SkillScanRule("download-and-invoke-expression", .remoteExecution,
                      #"(?i)((iex|invoke-expression)\b[^\n]{0,80}(iwr|invoke-webrequest|downloadstring|new-object\s+net\.webclient)|downloadstring\s*\(|(iwr|invoke-webrequest)\b[^\n]{0,80}\|\s*(iex|invoke-expression)\b)"#),
    ]
}
