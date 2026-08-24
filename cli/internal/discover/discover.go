// Package discover searches the public SkillNet skill index and maps its
// responses onto this project's own result shape.
//
// The index is a third-party read-only service reached with one
// unauthenticated GET. Two transport facts drive the code here:
//
//   - The endpoint is plain HTTP. SkillNet serves a certificate that does not
//     match the host, so HTTPS cannot be verified and queries travel in
//     plaintext. Only the search terms are ever sent.
//   - No credential of any kind is attached. The request carries no GitHub
//     token, no `gh` auth header, no cookie, and no registry content, so a
//     plaintext hop leaks nothing beyond the query itself.
//
// Search maps SkillNet's payload onto Response, which is the published
// contract the CLI's `--json` output, the TUI, and the macOS app all consume.
// SkillNet's own fields (the repository star count, per-score prose, cost and
// maintainability grades) are deliberately dropped: star counts belong to the
// host repository rather than the individual skill, and a stable narrow shape
// is what downstream surfaces need.
package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/nikships/skills-registry/cli/internal/registry"
)

const (
	// DefaultBaseURL is the public SkillNet search endpoint. Plain HTTP is
	// intentional: see the package comment.
	DefaultBaseURL = "http://api-skillnet.openkg.cn/v1/search"

	// BaseURLEnv overrides DefaultBaseURL. Tests point it at an httptest
	// server, and the macOS app can point it at a mirror.
	BaseURLEnv = "SKILLS_DISCOVER_URL"

	// SourceSkillNet labels every Response so a consumer that later gains a
	// second index can tell the rows apart.
	SourceSkillNet = "skillnet"

	// DefaultLimit and MaxLimit bound how many rows one search asks for. The
	// index holds tens of thousands of skills, so an unbounded limit would
	// return an unreadable page.
	DefaultLimit = 10
	MaxLimit     = 50

	// Timeout is the hard ceiling on one search, DNS through body read.
	Timeout = 10 * time.Second

	// userAgent identifies this CLI to the index. It carries no user
	// identity.
	userAgent = "skills-registry-discover"

	// maxBodyBytes caps how much of a response body is read, so a hostile or
	// broken endpoint cannot exhaust memory.
	maxBodyBytes = 8 << 20
)

// Mode selects SkillNet's ranking strategy: literal keyword matching or
// embedding similarity.
type Mode string

const (
	ModeKeyword Mode = "keyword"
	ModeVector  Mode = "vector"
)

// ParseMode validates a user-supplied --mode value.
func ParseMode(s string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(s))) {
	case ModeKeyword, "":
		return ModeKeyword, nil
	case ModeVector:
		return ModeVector, nil
	default:
		return "", fmt.Errorf("unknown mode %q (want %s or %s)", s, ModeKeyword, ModeVector)
	}
}

// Query is one search request.
type Query struct {
	// Text is the search string. Required.
	Text string
	// Mode defaults to ModeKeyword when empty.
	Mode Mode
	// Category, when set, restricts results to that SkillNet category.
	Category string
	// Limit is the requested row count, defaulted to DefaultLimit and capped
	// at MaxLimit.
	Limit int
}

// normalize fills in defaults and clamps Limit. Callers get the effective
// query back so the JSON payload reports what was actually asked for.
func (q Query) normalize() (Query, error) {
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" {
		return Query{}, errors.New("search query is empty")
	}
	mode, err := ParseMode(string(q.Mode))
	if err != nil {
		return Query{}, err
	}
	q.Mode = mode
	q.Category = strings.TrimSpace(q.Category)
	switch {
	case q.Limit <= 0:
		q.Limit = DefaultLimit
	case q.Limit > MaxLimit:
		q.Limit = MaxLimit
	}
	return q, nil
}

// Result is one skill in the index, in this project's shape.
//
// The three score fields carry SkillNet's `evaluation.<score>.level`
// (`Good`, `Average`, or `Poor`) and are empty when the index has no score.
// An empty score means unscored; it must never be rendered as a pass.
type Result struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Author        string `json:"author"`
	Category      string `json:"category"`
	SkillURL      string `json:"skill_url"`
	Safety        string `json:"safety"`
	Completeness  string `json:"completeness"`
	Executability string `json:"executability"`
}

// Response is the published search payload. Results is always non-nil so it
// encodes as `[]` rather than `null`.
type Response struct {
	Source  string   `json:"source"`
	Query   string   `json:"query"`
	Mode    string   `json:"mode"`
	Results []Result `json:"results"`
}

// Client searches one index endpoint.
type Client struct {
	// BaseURL is the search endpoint. Empty means BaseURL().
	BaseURL string
	// HTTPClient overrides the default client. Tests inject one with a short
	// timeout; production callers leave it nil.
	HTTPClient *http.Client
}

// New returns a Client bound to the configured endpoint.
func New() *Client {
	return &Client{BaseURL: BaseURL()}
}

// BaseURL resolves the endpoint from BaseURLEnv, falling back to
// DefaultBaseURL.
func BaseURL() string {
	if v := strings.TrimSpace(os.Getenv(BaseURLEnv)); v != "" {
		return v
	}
	return DefaultBaseURL
}

// Search runs one query and returns the mapped results.
//
// Every failure mode (unreachable host, timeout, non-2xx status, non-JSON
// body) returns an error with no partial Response: the caller is expected to
// fail closed rather than present a half-populated list.
func (c *Client) Search(ctx context.Context, q Query) (Response, error) {
	q, err := q.normalize()
	if err != nil {
		return Response{}, err
	}
	endpoint, err := searchURL(c.endpoint(), q)
	if err != nil {
		return Response{}, err
	}
	body, err := c.get(ctx, endpoint)
	if err != nil {
		return Response{}, err
	}
	var payload apiResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return Response{}, fmt.Errorf("skill index returned a non-JSON response: %w", err)
	}
	return Response{
		Source:  SourceSkillNet,
		Query:   q.Text,
		Mode:    string(q.Mode),
		Results: mapResults(payload.Data),
	}, nil
}

func (c *Client) endpoint() string {
	if strings.TrimSpace(c.BaseURL) != "" {
		return c.BaseURL
	}
	return BaseURL()
}

// get performs the search request. It builds the request itself rather than
// reusing any shared GitHub transport, so there is no path by which an
// Authorization header could reach the index.
func (c *Client) get(ctx context.Context, endpoint string) ([]byte, error) {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach the skill index at %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read the skill index response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("skill index returned HTTP %d: %s", resp.StatusCode, summarize(body))
	}
	return body, nil
}

// searchURL appends the query parameters to the endpoint, preserving any the
// endpoint already carries (an httptest server or a mirror may pin its own).
func searchURL(base string, q Query) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid skill index URL %q: %w", base, err)
	}
	params := u.Query()
	params.Set("q", q.Text)
	params.Set("mode", string(q.Mode))
	params.Set("limit", strconv.Itoa(q.Limit))
	if q.Category != "" {
		params.Set("category", q.Category)
	}
	u.RawQuery = params.Encode()
	return u.String(), nil
}

// summarize renders an error body as a single short line, so a 500 that
// returns an HTML page does not dump the page into the terminal.
func summarize(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if s == "" {
		return "(empty body)"
	}
	if r := []rune(s); len(r) > 120 {
		return string(r[:119]) + "…"
	}
	return s
}

// mapResults projects the index payload onto Result, dropping rows with no
// usable identity and collapsing duplicates.
//
// The index returns clones of the same skill when several repositories vendor
// it, so rows are deduplicated on (name, skill_url) with the first occurrence
// winning: that keeps the index's own ranking order intact.
func mapResults(data []apiSkill) []Result {
	out := make([]Result, 0, len(data))
	seen := make(map[[2]string]bool, len(data))
	for _, s := range data {
		r := Result{
			Name:          strings.TrimSpace(s.Name),
			Description:   strings.TrimSpace(s.Description),
			Author:        strings.TrimSpace(s.Author),
			Category:      strings.TrimSpace(s.Category),
			SkillURL:      strings.TrimSpace(s.SkillURL),
			Safety:        s.Evaluation.Safety.value(),
			Completeness:  s.Evaluation.Completeness.value(),
			Executability: s.Evaluation.Executability.value(),
		}
		if r.Name == "" && r.SkillURL == "" {
			continue
		}
		key := [2]string{r.Name, r.SkillURL}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

// SkillKey reduces a skill URL to an identity that survives a revision
// change: "owner/repo/path", lowercased. The index links a skill at whatever
// commit it last indexed, so the SHA in a URL the user pasted and the SHA in
// the index's own row routinely differ while naming the same folder. The bool
// is false for anything that is not a github.com folder URL.
func SkillKey(rawURL string) (string, bool) {
	target, ok := registry.ParseGitHubURL(rawURL)
	if !ok || !target.IsFolder() {
		return "", false
	}
	return strings.ToLower(target.FullName() + "/" + strings.Trim(target.Path, "/")), true
}

// Lookup finds the index's row for one skill folder URL, so a caller that
// already has a URL can read the grades the index assigned to it.
//
// The index has no lookup-by-URL endpoint, so this searches for the folder's
// own name and keeps the row whose SkillKey matches. A miss is not an error:
// the index simply has no row, which means unscored. The caller must treat
// unscored as unvetted rather than as a pass.
func (c *Client) Lookup(ctx context.Context, skillURL string) (Result, bool, error) {
	key, ok := SkillKey(skillURL)
	if !ok {
		return Result{}, false, nil
	}
	name := path.Base(key)
	if name == "." || name == "/" || name == "" {
		return Result{}, false, nil
	}
	resp, err := c.Search(ctx, Query{Text: name, Limit: MaxLimit})
	if err != nil {
		return Result{}, false, err
	}
	for _, r := range resp.Results {
		if got, ok := SkillKey(r.SkillURL); ok && got == key {
			return r, true, nil
		}
	}
	return Result{}, false, nil
}

// apiResponse is the subset of SkillNet's payload this package reads.
type apiResponse struct {
	Data []apiSkill `json:"data"`
}

type apiSkill struct {
	Name        string `json:"skill_name"`
	Description string `json:"skill_description"`
	Author      string `json:"author"`
	Category    string `json:"category"`
	SkillURL    string `json:"skill_url"`
	Evaluation  struct {
		Safety        apiScore `json:"safety"`
		Completeness  apiScore `json:"completeness"`
		Executability apiScore `json:"executability"`
	} `json:"evaluation"`
}

type apiScore struct {
	Level string `json:"level"`
}

// value returns the score's level verbatim. A missing level stays empty: an
// unscored skill must not be promoted to a passing grade.
func (s apiScore) value() string { return strings.TrimSpace(s.Level) }
