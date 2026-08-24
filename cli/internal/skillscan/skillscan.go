// Package skillscan is an offline heuristic scan of a SKILL.md for content
// that is hostile to the agent that will load it.
//
// A skill file is prose an agent reads as instructions, so a public skill can
// carry a prompt-injection payload, a credential-exfiltration recipe, or a
// pipe-to-shell installer with nothing about the file signalling danger. This
// package looks for the three shapes with regular expressions and reports what
// it matched.
//
// It is a warning layer, not a scanner in the antivirus sense. There is no
// model, no network call, and no sandbox: obfuscation, a payload split across
// lines, and anything expressed indirectly all pass. A clean result means
// "none of these patterns matched", never "this skill is safe". Callers must
// present findings as a prompt to read the source, and must keep the decision
// with the user.
//
// The rules are tuned to keep false positives low enough that a hit is worth
// reading: patterns that need a sink (a shell pipe, an HTTP POST) only fire
// when the sink is on the same line as the secret, and documentation-shaped
// lines such as `curl -H "Authorization: Bearer $API_KEY" https://api…` do not
// fire at all.
package skillscan

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Category groups findings by the kind of harm the pattern points at.
type Category string

const (
	// CategoryPromptInjection covers text aimed at the reading agent rather
	// than the user: instruction overrides, hidden-from-the-user directives,
	// and guardrail-disabling role play.
	CategoryPromptInjection Category = "prompt_injection"

	// CategoryCredentialExfiltration covers reading a secret (an SSH key, a
	// cloud credential file, the environment) and shipping it somewhere.
	CategoryCredentialExfiltration Category = "credential_exfiltration"

	// CategoryRemoteExecution covers downloading code and executing it in one
	// step, `curl … | sh` and its relatives.
	CategoryRemoteExecution Category = "remote_execution"
)

// Label renders a category for a human.
func (c Category) Label() string {
	switch c {
	case CategoryPromptInjection:
		return "prompt injection"
	case CategoryCredentialExfiltration:
		return "credential exfiltration"
	case CategoryRemoteExecution:
		return "remote code execution"
	default:
		return string(c)
	}
}

const (
	// MaxScanBytes caps how much of a file is scanned. A SKILL.md is prose;
	// past this the file is data, and scanning it would only slow an import.
	MaxScanBytes = 1 << 20

	// maxExcerptRunes bounds the quoted line in a finding so one long line
	// cannot flood the terminal.
	maxExcerptRunes = 140

	// maxFindings caps the reported findings. The point is to make the user
	// look at the file, and a dozen hits already does that.
	maxFindings = 24

	// maxPerRule caps how many times one rule reports, so a file that repeats
	// the same pattern does not crowd out the other categories.
	maxPerRule = 3
)

// Finding is one matched line.
type Finding struct {
	Category Category `json:"category"`
	// Rule is the stable identifier of the pattern that matched, so a
	// consumer can special-case one heuristic without parsing prose.
	Rule string `json:"rule"`
	// Line is the 1-based line number in the scanned file.
	Line int `json:"line"`
	// Excerpt is the matched line, whitespace-collapsed and length-capped.
	Excerpt string `json:"excerpt"`
}

// String renders one finding as a single line.
func (f Finding) String() string {
	return fmt.Sprintf("%s · line %d · %s · %q", f.Category.Label(), f.Line, f.Rule, f.Excerpt)
}

// rule is one heuristic. A rule fires when match hits a line, with (when set)
// also hits it, and unless (when set) does not.
type rule struct {
	name     string
	category Category
	match    *regexp.Regexp
	with     *regexp.Regexp
	unless   *regexp.Regexp
}

// fires reports whether the rule matches one line.
func (r rule) fires(line string) bool {
	if !r.match.MatchString(line) {
		return false
	}
	if r.with != nil && !r.with.MatchString(line) {
		return false
	}
	if r.unless != nil && r.unless.MatchString(line) {
		return false
	}
	return true
}

var (
	// exfilSink is a destination a secret could leave through: a pipe into a
	// network client, an HTTP body upload, a raw socket, or a webhook.
	exfilSink = regexp.MustCompile(`(?i)(\|\s*(curl|wget|nc|ncat|netcat|telnet|mail|sendmail|openssl)\b` +
		`|\bcurl\b[^|\n]*?(--data|--data-binary|--data-raw|-d\s|-F\s|--form|-T\s|--upload-file|-X\s*(POST|PUT))` +
		`|\bwget\b[^|\n]*--post-(data|file)` +
		`|/dev/tcp/` +
		`|requests\.(post|put)\(` +
		`|urllib\.request\.urlopen\(` +
		`|\bwebhook\b` +
		`|Invoke-(RestMethod|WebRequest)\b[^\n]*-Method\s*Post)`)

	// secretStore names a file or command that yields a long-lived credential.
	// Generic `API_KEY`-style names are deliberately absent: a skill that
	// documents its own API key is normal, and the shape that matters is
	// reading someone's stored credentials.
	secretStore = regexp.MustCompile(`(?i)(\.ssh/(id_[a-z0-9_]+|authorized_keys)` +
		`|\bid_(rsa|dsa|ecdsa|ed25519)\b` +
		`|\.aws/credentials` +
		`|\.config/gh/hosts\.ya?ml` +
		`|\bgh\s+auth\s+token\b` +
		`|\.netrc\b|\.npmrc\b|\.pypirc\b` +
		`|\.docker/config\.json` +
		`|\.kube/config\b` +
		`|security\s+find-(generic|internet)-password` +
		`|\.env(\.[a-z0-9]+)?\b` +
		`|BEGIN\s+(RSA\s+|OPENSSH\s+|EC\s+)?PRIVATE\s+KEY)`)

	// envDump names a bulk read of the process environment.
	envDump = regexp.MustCompile(`(?i)(\bprintenv\b|\benv\s*\||\bset\s*\|\s*(grep|curl)` +
		`|os\.environ\b|process\.env\b|Get-ChildItem\s+Env:|\bdotenv\b)`)
)

// rules is the full heuristic set. Every pattern is line-scoped: a payload
// split across lines is out of reach, which is stated in the package comment
// and in the user-facing docs.
var rules = []rule{
	{
		name:     "ignore-previous-instructions",
		category: CategoryPromptInjection,
		match: regexp.MustCompile(`(?i)\b(ignore|disregard|forget|override|discard)\b[^.!?\n]{0,48}?` +
			`\b(all\s+)?(previous|prior|preceding|earlier|above|foregoing|the\s+above)\b[^.!?\n]{0,48}?` +
			`\b(instruction|instructions|prompt|prompts|rule|rules|direction|directions|guideline|guidelines|context|message|messages)\b`),
	},
	{
		name:     "override-system-prompt",
		category: CategoryPromptInjection,
		match: regexp.MustCompile(`(?i)\b(ignore|disregard|override|bypass|replace|forget)\b[^.!?\n]{0,32}?` +
			`\b(system|developer|operator)\s+(prompt|message|instructions|rules)\b`),
	},
	{
		name:     "hide-from-user",
		category: CategoryPromptInjection,
		match: regexp.MustCompile(`(?i)(\b(do\s+not|don'?t|never)\b[^.!?\n]{0,32}?\b(tell|inform|notify|show|mention|reveal|disclose|report|display)\b[^.!?\n]{0,24}?\b(the\s+)?(user|human|operator|owner)\b` +
			`|\bwithout\s+(telling|informing|notifying|asking|alerting)\s+(the\s+)?(user|human|operator)\b` +
			`|\b(silently|covertly|secretly)\b[^.!?\n]{0,24}?\b(run|execute|send|upload|exfiltrate|install|delete)\b)`),
	},
	{
		name:     "disable-guardrails",
		category: CategoryPromptInjection,
		match: regexp.MustCompile(`(?i)(\b(disable|turn\s+off|switch\s+off|bypass|circumvent|ignore)\b[^.!?\n]{0,32}?\b(safety|safeguard|safeguards|guardrail|guardrails|restriction|restrictions|content\s+polic\w+|filter|filters|approval|permission\s+prompt)\b` +
			`|\byou\s+are\s+(now\s+)?(in\s+)?(dan\b|developer\s+mode|god\s+mode|jailbroken|unrestricted|unfiltered)` +
			`|\b(enter|enable|activate)\s+(dan|developer|god|unrestricted)\s+mode\b)`),
	},
	{
		name:     "secret-file-exfiltration",
		category: CategoryCredentialExfiltration,
		match:    secretStore,
		with:     exfilSink,
	},
	{
		name:     "environment-exfiltration",
		category: CategoryCredentialExfiltration,
		match:    envDump,
		with:     exfilSink,
	},
	{
		name:     "exfiltration-instruction",
		category: CategoryCredentialExfiltration,
		match: regexp.MustCompile(`(?i)(\bexfiltrat\w*\b` +
			`|\b(send|upload|post|forward|email|transmit|leak|copy)\b[^.!?\n]{0,48}?\b((the\s+|their\s+|his\s+|her\s+)?(user'?s?|users'?)\s+(\w+\s+){0,2}(api[\s_-]?keys?|secrets?|tokens?|passwords?|credentials?|private\s+keys?)` +
			`|contents\s+of\s+(the\s+)?[~/.\w-]*\.env\b` +
			`|(the\s+)?\.env\s+file\b` +
			`|[~/.\w-]*\.ssh/id_\w+))`),
	},
	{
		name:     "pipe-to-shell",
		category: CategoryRemoteExecution,
		match: regexp.MustCompile(`(?i)\b(curl|wget|fetch|iwr|invoke-webrequest)\b[^|\n]*\|\s*` +
			`(sudo\s+)?(env\s+\S+\s+)?(ba|z|k|da|c|fi)?sh\b`),
	},
	{
		name:     "pipe-to-interpreter",
		category: CategoryRemoteExecution,
		match: regexp.MustCompile(`(?i)\b(curl|wget|fetch)\b[^|\n]*\|\s*` +
			`(python3?|perl|ruby|node|osascript|pwsh|powershell)\s*(-\s|-$|$)`),
	},
	{
		name:     "eval-downloaded-code",
		category: CategoryRemoteExecution,
		match: regexp.MustCompile(`(?i)((eval|source|exec)\s*\(?\s*"?\$?\(\s*(curl|wget)\b` +
			`|\b(ba|z|k)?sh\s+(-[a-z]+\s+)*<\(\s*(curl|wget)\b` +
			`|\bpython3?\s+-c\s+["'][^"'\n]*urlopen\b)`),
	},
	{
		name:     "download-and-invoke-expression",
		category: CategoryRemoteExecution,
		match: regexp.MustCompile(`(?i)((iex|invoke-expression)\b[^\n]{0,80}(iwr|invoke-webrequest|downloadstring|new-object\s+net\.webclient)` +
			`|downloadstring\s*\(` +
			`|(iwr|invoke-webrequest)\b[^\n]{0,80}\|\s*(iex|invoke-expression)\b)`),
	},
}

// Scan reports every heuristic hit in text. The whole file is scanned,
// frontmatter included: a `description` an agent reads at load time is as good
// a carrier as the body.
//
// Findings are returned in file order. Results are bounded (maxPerRule per
// rule, maxFindings overall) so a hostile file cannot produce unbounded
// output.
func Scan(text string) []Finding {
	if len(text) > MaxScanBytes {
		text = text[:MaxScanBytes]
	}
	var out []Finding
	perRule := map[string]int{}
	for i, line := range strings.Split(text, "\n") {
		if len(out) >= maxFindings {
			break
		}
		for _, r := range rules {
			if perRule[r.name] >= maxPerRule || !r.fires(line) {
				continue
			}
			perRule[r.name]++
			out = append(out, Finding{
				Category: r.category,
				Rule:     r.name,
				Line:     i + 1,
				Excerpt:  excerpt(line),
			})
			if len(out) >= maxFindings {
				break
			}
		}
	}
	return out
}

// ScanFile scans one file, reading at most MaxScanBytes.
func ScanFile(path string) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(bufio.NewReader(io.LimitReader(f, MaxScanBytes)))
	if err != nil {
		return nil, err
	}
	return Scan(string(raw)), nil
}

// ScanSkill scans the SKILL.md inside a skill folder. A folder with no
// SKILL.md yields no findings and no error: discovery already decides what is
// a skill, and this package does not duplicate that judgement.
func ScanSkill(folder, mainFileName string) ([]Finding, error) {
	path := filepath.Join(folder, mainFileName)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return ScanFile(path)
}

// Categories returns the distinct categories present in findings, in the
// order they first appear. Used to summarize a hit without listing every line.
func Categories(findings []Finding) []Category {
	seen := map[Category]bool{}
	var out []Category
	for _, f := range findings {
		if seen[f.Category] {
			continue
		}
		seen[f.Category] = true
		out = append(out, f.Category)
	}
	return out
}

// Summary renders the categories present as a comma-separated phrase, for a
// one-line warning. Empty for no findings.
func Summary(findings []Finding) string {
	cats := Categories(findings)
	labels := make([]string, 0, len(cats))
	for _, c := range cats {
		labels = append(labels, c.Label())
	}
	return strings.Join(labels, ", ")
}

// excerpt collapses whitespace and caps length so a finding is one readable
// line regardless of the source formatting.
func excerpt(line string) string {
	s := strings.Join(strings.Fields(line), " ")
	if r := []rune(s); len(r) > maxExcerptRunes {
		return string(r[:maxExcerptRunes-1]) + "…"
	}
	return s
}
