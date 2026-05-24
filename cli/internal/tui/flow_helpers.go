package tui

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anand-92/skills-registry/cli/internal/scan"
)

type flowKey struct {
	k string
	d string
}

func flowFrame(title string, width, sparkleIdx int, body, footer string) string {
	hero := lipgloss.JoinHorizontal(lipgloss.Top,
		SparkleStyle.Render("✦"),
		" ",
		HeroStyle.Render(title),
		" ",
		SparkleStyle.Render("✧"),
	)
	barWidth := width - 2
	if barWidth <= 0 {
		barWidth = 40
	}
	panelWidth := width - 4
	if panelWidth < 48 {
		panelWidth = 48
	}
	panel := PanelFocused.Width(panelWidth).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left,
		hero,
		miniGradientBar(barWidth, sparkleIdx),
		"",
		panel,
		"",
		footer,
	)
}

func flowFooter(width, sparkleIdx int, keys []flowKey) string {
	parts := make([]string, 0, len(keys)*3)
	for i, kv := range keys {
		if i > 0 {
			parts = append(parts, KeySepStyle.Render(" · "))
		}
		parts = append(parts, KeyStyle.Render(kv.k), " ", KeyDescStyle.Render(kv.d))
	}
	left := strings.Join(parts, "")
	right := SubtitleStyle.Render(animationDots(sparkleIdx))
	if width <= 0 {
		return left
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
}

func newFlowConfirm(title, prompt, yesLabel string) ChoiceModel {
	return NewChoice(title, prompt, []Choice{
		{Value: "yes", Label: yesLabel, Hint: "Continue with the registry write"},
		{Value: "no", Label: "Cancel", Hint: "Make no changes"},
	})
}

func validateFlowSourceInput(source string) error {
	if !looksLikeLocalPath(source) {
		return nil
	}
	return validateSafeRelativePathInput(source, "source")
}

func validateFlowPublishPath(path string) error {
	return validateSafeRelativePathInput(path, "path")
}

func looksLikeLocalPath(input string) bool {
	return strings.HasPrefix(input, "./") ||
		strings.HasPrefix(input, "/") ||
		strings.HasPrefix(input, "../") ||
		strings.HasPrefix(input, "~") ||
		strings.Contains(input, `\`) ||
		isWindowsDrivePath(input)
}

func validateSafeRelativePathInput(input, label string) error {
	decoded, err := url.PathUnescape(input)
	if err != nil {
		return fmt.Errorf("invalid %s encoding: %w", label, err)
	}
	lowerInput := strings.ToLower(input)
	switch {
	case strings.Contains(decoded, `\`) || strings.Contains(lowerInput, "%5c"):
		return fmt.Errorf("invalid %s: backslashes are not allowed", label)
	case strings.Contains(lowerInput, "%2f"):
		return fmt.Errorf("invalid %s: encoded separators are not allowed", label)
	case strings.HasPrefix(decoded, "~"):
		return fmt.Errorf("invalid %s: tilde expansion is not allowed", label)
	case filepath.IsAbs(decoded) || isWindowsDrivePath(decoded):
		return fmt.Errorf("invalid %s: absolute paths are not allowed", label)
	}
	for _, segment := range strings.Split(filepath.ToSlash(decoded), "/") {
		if segment == ".." {
			return fmt.Errorf("invalid %s: traversal is not allowed", label)
		}
	}
	return nil
}

func redactSourceUserInfo(source string) string {
	parsed, err := url.Parse(source)
	if err != nil || parsed == nil || parsed.User == nil || parsed.Scheme == "" {
		return source
	}
	parsed.User = nil
	return parsed.String()
}

func isWindowsDrivePath(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	c := path[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func skillsToItems(skills []scan.Skill) []MultiSelectItem {
	items := make([]MultiSelectItem, 0, len(skills))
	for _, s := range skills {
		hint := s.Slug
		if s.Source != "" {
			hint += " · " + s.Source
		}
		items = append(items, MultiSelectItem{
			Value: s,
			Label: s.Name,
			Hint:  hint,
		})
	}
	return items
}

func valuesToSkills(values []any) []scan.Skill {
	out := make([]scan.Skill, 0, len(values))
	for _, v := range values {
		if skill, ok := v.(scan.Skill); ok {
			out = append(out, skill)
		}
	}
	return out
}

func filterExisting(skills []scan.Skill, existing map[string]struct{}) ([]scan.Skill, []string) {
	publishable := make([]scan.Skill, 0, len(skills))
	var skipped []string
	for _, sk := range skills {
		if _, dup := existing[sk.Slug]; dup {
			skipped = append(skipped, sk.Slug)
			continue
		}
		publishable = append(publishable, sk)
	}
	return publishable, skipped
}

func publishSkillSet(
	ctx context.Context,
	filesFn func(scan.Skill) (map[string][]byte, error),
	publishFn func(context.Context, string, map[string][]byte, string) (string, error),
	skills []scan.Skill,
	commitMsg func(string) string,
) ([]string, error) {
	if filesFn == nil || publishFn == nil {
		return nil, fmt.Errorf("publish flow is not configured")
	}
	pushed := make([]string, 0, len(skills))
	for _, sk := range skills {
		files, err := filesFn(sk)
		if err != nil {
			return nil, err
		}
		if _, err := publishFn(ctx, sk.Slug, files, commitMsg(sk.Slug)); err != nil {
			return nil, fmt.Errorf("publish %s: %w", sk.Slug, err)
		}
		pushed = append(pushed, sk.Slug)
	}
	return pushed, nil
}
