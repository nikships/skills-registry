package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/nikships/skills-registry/cli/internal/agents"
	"github.com/nikships/skills-registry/cli/internal/config"
	"github.com/nikships/skills-registry/cli/internal/importgate"
	"github.com/nikships/skills-registry/cli/internal/jsonout"
	"github.com/nikships/skills-registry/cli/internal/registry"
	"github.com/nikships/skills-registry/cli/internal/scan"
	"github.com/nikships/skills-registry/cli/internal/trust"
	"github.com/nikships/skills-registry/cli/internal/tui"
)

// addJSONResult is the payload emitted by `add --json [--yes]`.
// Mirrors syncJSONResult so an agent driving both commands sees a
// consistent {pushed, skipped} shape. `skipped` carries slugs that
// were discovered inside the source but already exist in the registry
// (the safe "no-op" path) so the consumer can decide whether to flag
// drift. `installed` maps each published slug to the list of absolute
// paths the durable install copied into, allowing the consumer to
// correlate trivially via map lookup (e.g. looking up a slug from `pushed`).
//
// The remaining fields report the import gate, so a consumer never has to
// infer from a short `pushed` array that something was held back:
//
//   - `source` names where the skill came from and whether that origin is
//     trusted. It is always present.
//   - `refused` lists every skill that was NOT published, each with the
//     grades and scan findings behind the refusal. A non-empty `refused`
//     accompanies a non-zero exit, so a scripted caller can fail on either.
//   - `install_skipped` is true when a durable install was deliberately not
//     performed because the source is untrusted and --install was absent.
type addJSONResult struct {
	Pushed         []string            `json:"pushed"`
	Skipped        []string            `json:"skipped"`
	Installed      map[string][]string `json:"installed,omitempty"`
	Source         addJSONSource       `json:"source"`
	Refused        []importgate.Review `json:"refused,omitempty"`
	InstallSkipped bool                `json:"install_skipped"`
	// InstallSkippedReason explains InstallSkipped in prose, empty when no
	// install was skipped.
	InstallSkippedReason string `json:"install_skipped_reason,omitempty"`
}

// addJSONSource is the gate's view of the source in the JSON payload.
type addJSONSource struct {
	Origin    string `json:"origin"`
	Untrusted bool   `json:"untrusted"`
	Reason    string `json:"reason"`
}

// addOptions carries one `add` invocation's flags. Grouped into a struct
// because the gate needs most of them and a six-argument call site says
// nothing about which bool is which.
type addOptions struct {
	// yes skips the ordinary publish confirmation. It does NOT clear a gate
	// block: agreeing to skip prompts is not agreeing to import a skill
	// graded Poor for safety.
	yes bool
	// all publishes every skill found in the source without the multi-select.
	all bool
	// install opts an untrusted import into the durable agent-folder install.
	// A trusted source keeps its existing behavior and ignores this.
	install bool
	// allowUnsafe is the escape hatch past a Poor safety grade or a local
	// scan hit.
	allowUnsafe bool
	// fromDiscover marks a source handed over by the public index, which is
	// untrusted whatever shape its URL has.
	fromDiscover bool
}

var windowsDrivePathRe = regexp.MustCompile(`^[A-Za-z]:`)

func newAddCmd() *cobra.Command {
	var opts addOptions
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Add skills from an external source (path, owner/repo, git URL, or GitHub folder URL) to the registry",
		Long: `Resolves the source, discovers every SKILL.md inside it, lets you
multi-select what to publish, and pushes the selected skills to your GitHub
registry repo.

Accepted sources:

  ./path/to/skills                                a local directory
  owner/repo                                      shallow clone of the whole repo
  https://github.com/owner/repo/tree/<ref>/<dir>  fetch only that folder
  https://github.com/owner/repo/blob/<sha>/<dir>  same, SHA-pinned
  https://gitlab.com/owner/repo.git               any other git URL, cloned

A GitHub folder URL is fetched through the GitHub Contents API with your
existing gh credentials, so importing one skill out of a large monorepo
never clones the repository.

Untrusted sources are gated. A local folder, and a repository under your own
registry's owner, are trusted and behave as before: they publish and durably
install into the agent dot-folders you pick. Anything else — a public GitHub
folder URL, a third-party owner/repo, or a row out of ` + "`discover`" + ` — is
treated as a stranger's SKILL.md:

  · it publishes to your registry and stops there. Pass --install to also copy
    it into agent dot-folders, where every agent would load it each session.
  · the public index's safety, completeness, and executability grades are
    shown first; a grade the index never assigned reads as "` + importgate.UnscoredLabel + `",
    which means unvetted, not safe.
  · a Poor safety grade, or a hit from the local heuristic scan of SKILL.md,
    needs --allow-unsafe (or an extra confirmation interactively).

Nothing fetched is ever executed. Files under scripts/ are copied, never run.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A refused import, an unreachable source, or a failed publish is
			// an outcome rather than a misuse of the command, so neither the
			// usage block nor cobra's own error line belongs in the output;
			// main prints the error once. Argument-count validation still
			// shows usage because it runs before RunE.
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			if jsonout.Enabled() {
				return runAddJSON(cmd.Context(), args[0], opts)
			}
			opts.yes = opts.yes || shouldAutoYes()
			return runAdd(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false,
		"Skip the publish confirmation. Does not clear a safety block.")
	cmd.Flags().BoolVar(&opts.all, "all", false, "Publish every skill found in the source.")
	cmd.Flags().BoolVar(&opts.install, "install", false,
		"Also install into agent dot-folders. Required for an untrusted source; trusted sources prompt as before.")
	cmd.Flags().BoolVar(&opts.allowUnsafe, "allow-unsafe", false,
		"Import despite a Poor safety grade or a local scan hit.")
	cmd.Flags().BoolVar(&opts.fromDiscover, "from-discover", false,
		"Mark the source as a public-index pick, so the untrusted gate applies whatever its URL shape.")
	return cmd
}

// addPlan is the resolved state shared by the JSON and interactive paths:
// the registry client, the candidate skills, the slugs already published, and
// the import gate's verdict for the source.
type addPlan struct {
	cfg    config.Config
	client *registry.Client
	gate   gate
	// root is the directory the source resolved into: the local folder, the
	// clone root, or the temp dir a folder fetch wrote. Provenance stamping
	// needs it to locate each skill folder within the source.
	root    string
	source  string
	missing []scan.Skill
	skipped []string
}

// planAdd resolves the source, discovers its skills, dedupes against the
// registry, and builds the import gate. The gate is built before anything is
// published, so an untrusted source is reviewed while nothing has been written
// yet.
//
// The returned cleanup removes the fetched temp dir and is never nil.
func planAdd(ctx context.Context, source string, opts addOptions) (addPlan, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return addPlan{}, noopCleanup, err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return addPlan{}, noopCleanup, err
	}
	dir, cleanup, err := resolveSource(ctx, source)
	if err != nil {
		return addPlan{}, noopCleanup, err
	}
	skills, err := scan.Discover([]scan.Source{{Path: dir, Label: source}})
	if err != nil {
		cleanup()
		return addPlan{}, noopCleanup, err
	}
	if len(skills) == 0 {
		cleanup()
		return addPlan{}, noopCleanup, fmt.Errorf("no SKILL.md files found under %s", source)
	}
	existing, err := client.Slugs(ctx)
	if err != nil {
		cleanup()
		return addPlan{}, noopCleanup, err
	}
	// Mismatches (a slug differing from the registry's only by separators or
	// case) are already in the registry, so they are reported as skipped
	// rather than republished under a second name.
	missing, _ := scan.DedupeAgainst(skills, existing)
	missingSet := map[string]struct{}{}
	for _, sk := range missing {
		missingSet[sk.Slug] = struct{}{}
	}
	skipped := []string{}
	for _, sk := range skills {
		if _, ok := missingSet[sk.Slug]; !ok {
			skipped = append(skipped, sk.Slug)
		}
	}
	g, err := buildGate(ctx, source, cfg, missing, opts.fromDiscover)
	if err != nil {
		cleanup()
		return addPlan{}, noopCleanup, err
	}
	return addPlan{
		cfg:     cfg,
		client:  client,
		gate:    g,
		root:    dir,
		source:  source,
		missing: missing,
		skipped: skipped,
	}, cleanup, nil
}

// stamp writes the provenance keys onto the SKILL.md of each skill about to be
// published, for an untrusted source only. Called after the gate has reviewed
// the upstream files and before the first registry write, so the scan reads the
// stranger's file and the registry receives the annotated copy.
func (p addPlan) stamp(skills []scan.Skill) error {
	return stampProvenance(p.gate.untrusted(), p.source, p.root, p.gate.category, skills)
}

// jsonSource renders the gate's assessment for the JSON payload.
func (p addPlan) jsonSource() addJSONSource {
	return addJSONSource{
		Origin:    string(p.gate.assessment.Origin),
		Untrusted: p.gate.assessment.Untrusted,
		Reason:    p.gate.assessment.Reason,
	}
}

// runAddJSON is the --json code path: skips the multi-select prompt and
// publishes every SKILL.md found in the resolved source that isn't already in
// the registry. Failures surface as {"error": "..."} + a non-zero exit.
//
// The import gate applies here too, and non-interactively it can only be
// cleared by a flag:
//
//   - an untrusted source does not durable-install unless --install is set,
//     and the payload's install_skipped says so;
//   - a skill blocked by a Poor safety grade or a local scan hit is refused
//     unless --allow-unsafe is set. The refusal is both an explicit `refused`
//     entry and a non-zero exit, so neither a payload reader nor an exit-code
//     checker can mistake it for success.
func runAddJSON(ctx context.Context, source string, opts addOptions) error {
	plan, cleanup, err := planAdd(ctx, source, opts)
	if err != nil {
		jsonout.PrintError(err)
		return err
	}
	defer cleanup()

	allowed, refused := allowedSkills(plan.gate, plan.missing, opts.allowUnsafe)
	untrusted := plan.gate.untrusted()
	// A refusal must not half-publish: with anything blocked, nothing is
	// written and the caller is told what to pass to proceed.
	if len(refused) > 0 {
		err := blockedError(refused)
		jsonout.PrintError(err)
		return err
	}

	if err := plan.stamp(allowed); err != nil {
		jsonout.PrintError(err)
		return err
	}

	installTargets := jsonInstallTargets(untrusted, opts.install)
	installed := map[string][]string{}
	pushed := []string{}
	safeSource := redactSourceUserInfo(source)
	for _, sk := range allowed {
		files := map[string][]byte{}
		if err := walkSkillIntoFiles(sk, files); err != nil {
			jsonout.PrintError(err)
			return err
		}
		bySlug := rekeyBySlug(sk.Slug, files)
		msg := fmt.Sprintf("add: %s (from %s)", sk.Slug, safeSource)
		if _, err := plan.client.Publish(ctx, sk.Slug, bySlug, msg); err != nil {
			err = fmt.Errorf("publish %s: %w", sk.Slug, err)
			jsonout.PrintError(err)
			return err
		}
		pushed = append(pushed, sk.Slug)
		if len(installTargets) == 0 {
			continue
		}
		paths, err := installSkillIntoTargets(ctx, plan.client, sk.Slug, installTargets)
		if err != nil {
			err = fmt.Errorf("install %s locally: %w", sk.Slug, err)
			jsonout.PrintError(err)
			return err
		}
		installed[sk.Slug] = paths
	}
	out := addJSONResult{
		Pushed:    pushed,
		Skipped:   plan.skipped,
		Installed: installed,
		Source:    plan.jsonSource(),
	}
	if untrusted && !opts.install {
		out.InstallSkipped = true
		out.InstallSkippedReason = fmt.Sprintf(
			"%s is untrusted (%s), so no agent dot-folder was written; pass %s to install",
			redactSourceUserInfo(source), plan.gate.assessment.Reason, installFlag)
	}
	return jsonout.Print(out)
}

// jsonInstallTargets decides where the non-interactive path installs. A
// trusted source keeps the historical always-install behavior; an untrusted
// one installs only when the user passed --install.
func jsonInstallTargets(untrusted, install bool) []agents.Target {
	if untrusted && !install {
		return nil
	}
	return universalInstallTargets()
}

func runAdd(ctx context.Context, source string, opts addOptions) error {
	plan, cleanup, err := planAdd(ctx, source, opts)
	if err != nil {
		return err
	}
	defer cleanup()

	renderGate(os.Stdout, plan.gate)
	ok, err := confirmUntrusted(plan.gate, opts)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("Cancelled.")
		return nil
	}
	// Every block was cleared above, by confirmUntrusted's extra prompt or by
	// --allow-unsafe, so the full candidate set reaches the multi-select. The
	// interactive path refuses nothing silently: a user who saw the warning
	// and said yes gets to choose from everything the source offered.
	picked, err := selectSkillsForAdd(plan.missing, opts.yes, opts.all, source, plan.cfg.Repo)
	if err != nil {
		return err
	}
	if picked == nil {
		return nil
	}
	if len(picked) == 0 {
		fmt.Println("Nothing selected.")
		return nil
	}

	targets, cancelled, err := resolveInstallTargets(plan.gate, opts, len(picked))
	if err != nil {
		return err
	}
	if cancelled {
		fmt.Println("Cancelled.")
		return nil
	}

	if err := plan.stamp(picked); err != nil {
		return err
	}
	safeSource := redactSourceUserInfo(source)
	if err := publishSkills(ctx, plan.client, picked, func(slug string) string {
		return fmt.Sprintf("add: %s (from %s)", slug, safeSource)
	}); err != nil {
		return err
	}
	if len(targets) == 0 {
		fmt.Println(tui.HintStyle.Render(
			"Registry updated. No agent dot-folder was written; pass " + installFlag + " to install."))
		return nil
	}
	return installPickedLocally(ctx, plan.client, picked, targets)
}

// resolveInstallTargets decides which agent dot-folders receive a durable
// install, and reports whether the user cancelled at the picker.
//
// A trusted source behaves as it always has: the picker opens (or --yes takes
// the locked-universal set). An untrusted source installs nothing unless the
// user asked for it, either with --install or by answering yes to the extra
// prompt; only then does the picker open.
func resolveInstallTargets(g gate, opts addOptions, count int) ([]agents.Target, bool, error) {
	if !g.untrusted() {
		targets, err := promptInstallTargets(opts.yes, count)
		return targets, targets == nil && err == nil, err
	}
	if !opts.install {
		if opts.yes {
			return nil, false, nil
		}
		wanted, err := confirmChoice(
			fmt.Sprintf("Also install %d untrusted skill(s) into agent folders?", count),
			"Every agent then loads this SKILL.md each session. The registry write happens either way.",
			"No, registry only (recommended)",
			"Yes, choose agent folders",
		)
		if err != nil || !wanted {
			return nil, false, err
		}
	}
	targets, err := promptInstallTargets(opts.yes, count)
	return targets, targets == nil && err == nil, err
}

// promptInstallTargets asks the user which agent dot-folders should
// receive a durable install of every just-published skill. `yes`
// (--yes or --json auto-yes) skips the picker and defaults to the
// locked-universal set so a scripted `add --yes` keeps publishing +
// installing without a TTY prompt.
func promptInstallTargets(yes bool, count int) ([]agents.Target, error) {
	if yes {
		return universalInstallTargets(), nil
	}
	subtitle := fmt.Sprintf("%d skill(s) just published", count)
	picker := tui.NewInstallPicker(
		"Install locally into which agents?",
		subtitle,
		installPickerTargets(),
	).AsStandalone()
	out, err := tea.NewProgram(picker).Run()
	if err != nil {
		return nil, err
	}
	final := out.(tui.InstallPickerModel)
	if final.Cancelled() {
		fmt.Println("Cancelled.")
		return nil, nil
	}
	targets, err := installAnyValuesToTargets(final.SelectedValues())
	if err != nil {
		return nil, err
	}
	return targets, nil
}

// installPickedLocally durably installs every published skill into the
// supplied targets. Failures are surfaced immediately — once any local
// install fails the loop stops, matching publishSkills' fail-fast
// contract.
func installPickedLocally(ctx context.Context, client *registry.Client, picked []scan.Skill, targets []agents.Target) error {
	for _, sk := range picked {
		paths, err := installSkillIntoTargets(ctx, client, sk.Slug, targets)
		if err != nil {
			return fmt.Errorf("install %s locally: %w", sk.Slug, err)
		}
		switch len(paths) {
		case 0:
		case 1:
			fmt.Println(tui.OkStyle.Render("→"), sk.Slug, tui.HintStyle.Render(paths[0]))
		default:
			fmt.Println(tui.OkStyle.Render("→"), sk.Slug,
				tui.HintStyle.Render(fmt.Sprintf("installed into %d agents", len(paths))))
		}
	}
	return nil
}

// selectSkillsForAdd handles the interactive multi-select and confirmation
// for add. Returns nil with no error when the user cancels or selects nothing.
func selectSkillsForAdd(skills []scan.Skill, yes, all bool, source, repo string) ([]scan.Skill, error) {
	if all {
		return skills, nil
	}
	picked, err := promptAddSelection(skills)
	if err != nil {
		if strings.Contains(err.Error(), "cancelled") {
			return nil, nil
		}
		return nil, err
	}
	if len(picked) == 0 {
		return []scan.Skill{}, nil
	}
	if !yes {
		ok, err := confirmPush(fmt.Sprintf(
			"Publish %d skill(s) from %s to %s?", len(picked), source, repo))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
	}
	return picked, nil
}

func promptAddSelection(skills []scan.Skill) ([]scan.Skill, error) {
	items := make([]tui.MultiSelectItem, 0, len(skills))
	for _, s := range skills {
		items = append(items, tui.MultiSelectItem{
			Value: s,
			Label: s.Name,
			Hint:  s.Slug,
		})
	}
	model := tui.NewMultiSelect("Select skills to publish", items, nil, true)
	out, err := tea.NewProgram(model).Run()
	if err != nil {
		return nil, err
	}
	final := out.(tui.MultiSelectModel)
	if final.Cancelled() {
		return nil, fmt.Errorf("cancelled")
	}
	var picked []scan.Skill
	for _, v := range final.SelectedValues() {
		picked = append(picked, v.(scan.Skill))
	}
	return picked, nil
}

func resolveSource(ctx context.Context, source string) (string, func(), error) {
	return resolveSourceWithNotice(ctx, source, !jsonout.Enabled())
}

func resolveSourceQuiet(ctx context.Context, source string) (string, func(), error) {
	return resolveSourceWithNotice(ctx, source, false)
}

func resolveSourceWithNotice(ctx context.Context, source string, announce bool) (string, func(), error) {
	if isLocalSourcePath(source) {
		return resolveLocalSource(source)
	}
	target, isGitHub := registry.ParseGitHubURL(source)
	if isGitHub && target.IsFolder() {
		return fetchGitHubFolder(ctx, target, announce)
	}
	url, ref := cloneURLAndRef(source, target, isGitHub)
	return cloneSource(ctx, url, ref, announce)
}

func isLocalSourcePath(source string) bool {
	return trust.IsLocalPath(source)
}

func resolveLocalSource(source string) (string, func(), error) {
	path, err := validateLocalSourcePath(source)
	if err != nil {
		return "", noopCleanup, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", noopCleanup, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", noopCleanup, fmt.Errorf("not a directory: %s", source)
	}
	return abs, noopCleanup, nil
}

// cloneURLAndRef maps a non-folder source to the URL git should clone and the
// branch to pin, if any. `owner/repo` shorthand expands to a GitHub HTTPS
// remote; a github.com repo or `/tree/<branch>` link cannot be handed to git
// verbatim, so it becomes the clone URL plus its branch; anything else (GitLab,
// `git@…`) is cloned as-is. A full commit SHA is dropped because
// `git clone --branch <sha>` fails.
func cloneURLAndRef(source string, target registry.GitHubTarget, isGitHub bool) (string, string) {
	owner, repo, isShorthand := trust.ParseOwnerRepo(source)
	switch {
	case isShorthand:
		return "https://github.com/" + owner + "/" + repo + ".git", ""
	case isGitHub && target.RefIsSHA():
		return target.CloneURL(), ""
	case isGitHub:
		return target.CloneURL(), target.Ref
	default:
		return source, ""
	}
}

func cloneSource(ctx context.Context, url, ref string, announce bool) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "skills-registry-add-")
	if err != nil {
		return "", noopCleanup, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if announce {
		fmt.Println(tui.HintStyle.Render("cloning " + url + " …"))
	}
	args := []string{"clone", "--depth", "1", "--single-branch"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, tmp)
	cmd := exec.CommandContext(ctx, "git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", noopCleanup, fmt.Errorf("git clone failed: %s", strings.TrimSpace(string(out)))
	}
	return tmp, cleanup, nil
}

// newFolderFetcher builds the GitHub Contents-API fetcher used for folder
// URLs. Tests swap it for one backed by a fake `gh` runner.
var newFolderFetcher = func() (*registry.Fetcher, error) { return registry.NewFetcher() }

// fetchGitHubFolder downloads only `target`'s folder into a temp dir and
// returns that dir as the source root, so the caller's existing discover →
// select → publish pipeline runs unchanged. The parent repository is never
// cloned, which is what makes importing one skill out of a monorepo viable.
func fetchGitHubFolder(ctx context.Context, target registry.GitHubTarget, announce bool) (string, func(), error) {
	fetcher, err := newFolderFetcher()
	if err != nil {
		return "", noopCleanup, err
	}
	tmp, err := os.MkdirTemp("", "skills-registry-add-")
	if err != nil {
		return "", noopCleanup, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if announce {
		fmt.Println(tui.HintStyle.Render("fetching " + target.Path + " from " + target.FullName() + " …"))
	}
	folder, err := fetcher.FetchFolder(ctx, target, tmp)
	if err != nil {
		cleanup()
		return "", noopCleanup, err
	}
	if !containsSkillFile(folder.Paths) {
		cleanup()
		return "", noopCleanup, fmt.Errorf(
			"%s has no %s (found %d file(s)); point the URL at a skill folder or its parent",
			folder.Target.WebURL(), scan.MainFileName, len(folder.Paths))
	}
	return tmp, cleanup, nil
}

// containsSkillFile reports whether any fetched path is a SKILL.md, at the
// folder root or nested one level down (a folder of skills).
func containsSkillFile(paths []string) bool {
	for _, p := range paths {
		if filepath.Base(p) == scan.MainFileName {
			return true
		}
	}
	return false
}

func validateLocalSourcePath(source string) (string, error) {
	path, err := url.PathUnescape(source)
	if err != nil {
		return "", fmt.Errorf("invalid source path encoding: %w", err)
	}
	lowerSource := strings.ToLower(source)
	switch {
	case strings.Contains(path, `\`) || strings.Contains(lowerSource, "%5c"):
		return "", fmt.Errorf("invalid source path: backslashes are not allowed")
	case strings.Contains(lowerSource, "%2f"):
		return "", fmt.Errorf("invalid source path: encoded separators are not allowed")
	case strings.HasPrefix(path, "~"):
		return "", fmt.Errorf("invalid source path: tilde expansion is not allowed")
	case filepath.IsAbs(path) || windowsDrivePathRe.MatchString(path):
		return "", fmt.Errorf("invalid source path: absolute paths are not allowed")
	}
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if segment == ".." {
			return "", fmt.Errorf("invalid source path: traversal is not allowed")
		}
	}
	return path, nil
}

func redactSourceUserInfo(source string) string {
	parsed, err := url.Parse(source)
	if err != nil || parsed == nil || parsed.User == nil || parsed.Scheme == "" {
		return source
	}
	parsed.User = nil
	return parsed.String()
}

func noopCleanup() {}
