package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type PublishFlowDeps struct {
	Publish func(context.Context, string) (PublishFlowResult, error)
}

type PublishFlowResult struct {
	Slug string
	Repo string
	SHA  string
	URL  string
}

type publishFlowState int

const (
	publishStatePath publishFlowState = iota
	publishStatePublishing
)

type PublishFlowModel struct {
	ctx  context.Context
	deps PublishFlowDeps

	state   publishFlowState
	path    InputModel
	spinner spinner.Model

	width, height int
	sparkleIdx    int
	pathText      string
}

type publishFlowDoneMsg struct {
	result PublishFlowResult
	err    error
}

func NewPublishFlow(ctx context.Context, deps PublishFlowDeps) PublishFlowModel {
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	input := NewInput("Publish a local skill", "", "path to folder containing SKILL.md", "")
	input.Help = "enter to publish · esc to cancel"
	return PublishFlowModel{
		ctx:     ctx,
		deps:    deps,
		state:   publishStatePath,
		path:    input,
		spinner: sp,
	}
}

func (m PublishFlowModel) Init() tea.Cmd {
	return tea.Batch(sparkleTick(), m.path.Init())
}

func (m PublishFlowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case sparkleTickMsg:
		m.sparkleIdx++
		return m, sparkleTick()
	case spinner.TickMsg:
		if m.state != publishStatePublishing {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case publishFlowDoneMsg:
		return m.handleDone(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m.forward(msg)
}

func (m PublishFlowModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.state != publishStatePath {
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c", "esc":
		return m, flowExitCmd("publish · cancelled", true)
	case "enter":
		path := strings.TrimSpace(m.path.Value())
		if path == "" {
			m.path.err = fmt.Errorf("path is required")
			return m, nil
		}
		if err := validateFlowPublishPath(path); err != nil {
			m.path.err = err
			return m, nil
		}
		m.pathText = path
		m.state = publishStatePublishing
		return m, tea.Batch(m.spinner.Tick, m.startPublish(path))
	}
	return m.forward(msg)
}

func (m PublishFlowModel) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.path.Update(msg)
	m.path = next.(InputModel)
	return m, cmd
}

func (m PublishFlowModel) startPublish(path string) tea.Cmd {
	return func() tea.Msg {
		if m.deps.Publish == nil {
			return publishFlowDoneMsg{err: fmt.Errorf("publish flow is not configured")}
		}
		result, err := m.deps.Publish(m.ctx, path)
		return publishFlowDoneMsg{result: result, err: err}
	}
}

func (m PublishFlowModel) handleDone(msg publishFlowDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, flowExitCmd("✗ publish: "+flattenErr(msg.err), false)
	}
	return m, flowExitCmd(fmt.Sprintf("✓ published %s to %s@%s",
		msg.result.Slug, msg.result.Repo, shortFlowSHA(msg.result.SHA)), true)
}

func (m PublishFlowModel) View() string {
	return flowFrame("Skills Registry · Publish", m.width, m.sparkleIdx, m.renderBody(), m.renderFooter())
}

func (m PublishFlowModel) renderBody() string {
	if m.state == publishStatePublishing {
		return m.spinner.View() + " " + lipgloss.NewStyle().Foreground(ColInk).
			Render("Publishing "+m.pathText+" …")
	}
	return m.path.View()
}

func (m PublishFlowModel) renderFooter() string {
	if m.state == publishStatePublishing {
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"wait", "publishing"}})
	}
	return flowFooter(m.width, m.sparkleIdx, []flowKey{{"type", "path"}, {"enter", "publish"}, {"esc", "cancel"}})
}

func shortFlowSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
