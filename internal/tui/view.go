package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/shaheeranser/watcher/internal/api"
	"github.com/shaheeranser/watcher/internal/store"
)

// styles carries every visual choice, built once so degradation is a single
// decision rather than a check in each render (DASH-NFR-4).
type styles struct {
	degraded bool
	title    lipgloss.Style
	dim      lipgloss.Style
	selected lipgloss.Style
	footer   lipgloss.Style
	severity map[string]lipgloss.Style
}

func newStyles(degraded bool) styles {
	if degraded {
		plain := lipgloss.NewStyle()
		return styles{
			degraded: true,
			title:    plain,
			dim:      plain,
			selected: plain,
			footer:   plain,
			severity: map[string]lipgloss.Style{},
		}
	}
	return styles{
		title:    lipgloss.NewStyle().Bold(true),
		dim:      lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		selected: lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("238")),
		footer:   lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		severity: map[string]lipgloss.Style{
			"critical": lipgloss.NewStyle().Foreground(lipgloss.Color("197")).Bold(true),
			"high":     lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
			"medium":   lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
			"low":      lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		},
	}
}

func (s styles) severityOf(sev string) lipgloss.Style {
	if style, ok := s.severity[strings.ToLower(sev)]; ok {
		return style
	}
	if s.degraded {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
}

// View renders the dashboard. It reads only model state and performs no I/O.
func (m Model) View() string {
	if m.help {
		return m.renderHelp()
	}
	if m.width < minWidth || m.height < minHeight {
		return m.renderTooSmall()
	}
	listH := m.listHeight()
	detailH := m.height - 1 - listH
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderList(listH),
		m.renderDetail(detailH),
		m.renderFooter(),
	)
}

func (m Model) renderList(height int) string {
	lines := make([]string, 0, height)
	lines = append(lines, m.styles.title.Render(padRight(clip(m.listHeader(), m.width), m.width)))

	body := height - 1
	start := 0
	if m.selected >= body {
		start = m.selected - body + 1
	}
	if maxStart := len(m.rows) - body; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	for i := start; i < len(m.rows) && len(lines) < height; i++ {
		lines = append(lines, m.renderRow(i))
	}
	for len(lines) < height {
		lines = append(lines, padRight("", m.width))
	}
	return strings.Join(lines, "\n")
}

func (m Model) listHeader() string {
	active, resolved := 0, 0
	for _, r := range m.rows {
		if r.State == string(store.StateResolved) {
			resolved++
		} else {
			active++
		}
	}
	if m.degraded {
		return fmt.Sprintf("Incidents  %d active, %d resolved", active, resolved)
	}
	return fmt.Sprintf("Incidents  %d active · %d resolved", active, resolved)
}

func (m Model) renderRow(i int) string {
	row := m.rows[i]
	severity := strings.ToUpper(row.Severity)
	if severity == "" {
		severity = "—"
	}
	prefix := fmt.Sprintf("%s %-8s %-13s %-11s x%-4d %-7s ",
		m.glyph(row), clip(severity, 8), clip(row.Kind, 13), clip(row.Source, 11),
		row.Count, ageOf(row.LastSeen, m.now()))
	line := padRight(prefix+clip(m.rowSummary(row), m.width-lipgloss.Width(prefix)), m.width)

	switch {
	case i == m.selected && m.focus == FocusList:
		return m.styles.selected.Render(line)
	case row.State == string(store.StateResolved):
		return m.styles.dim.Render(line)
	default:
		return m.styles.severityOf(row.Severity).Render(line)
	}
}

func (m Model) rowSummary(row api.IncidentRow) string {
	switch {
	case pending(row):
		return m.marker("analysing", "⏳ analysing")
	case unavailable(row):
		return m.marker("explanation unavailable", "⚠ explanation unavailable")
	case row.Summary == "":
		return "—"
	default:
		return row.Summary
	}
}

func (m Model) glyph(row api.IncidentRow) string {
	active := row.State != string(store.StateResolved)
	switch {
	case m.degraded && active:
		return "*"
	case m.degraded:
		return "o"
	case active:
		return "●"
	default:
		return "○"
	}
}

func (m Model) renderDetail(height int) string {
	lines := []string{m.styles.title.Render(padRight(clip(m.detailHeader(), m.width), m.width))}

	content := m.detailLines()
	visible := height - 1
	if maxScroll := len(content) - visible; m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	for i := m.scroll; i < len(content) && len(lines) < height; i++ {
		lines = append(lines, clip(content[i], m.width))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) detailHeader() string {
	if m.detail == nil {
		if m.detailID == "" {
			return "Detail"
		}
		return "Detail  " + m.detailID
	}
	return fmt.Sprintf("Detail  %s  %s", m.detail.Fingerprint, m.detail.State)
}

func (m Model) detailLines() []string {
	d := m.detail
	if d == nil {
		return []string{"", m.styles.dim.Render("loading…")}
	}

	lines := []string{
		field("Summary", m.detailSummary(*d)),
		field("Likely cause", valueOr(d.LikelyCause, "—")),
	}
	if len(d.Evidence) == 0 {
		lines = append(lines, field("Evidence", "—"))
	} else {
		for i, ev := range d.Evidence {
			label := "Evidence"
			if i > 0 {
				label = ""
			}
			lines = append(lines, field(label, "> "+ev))
		}
	}
	lines = append(lines,
		field("Suggested fix", valueOr(d.SuggestedFix, "—")),
		field("Confidence", fmt.Sprintf("%.2f   Severity %s", d.Confidence, valueOr(strings.ToUpper(d.Severity), "—"))),
		field("State", fmt.Sprintf("%s   Count %d", valueOr(d.State, "—"), d.Count)),
		field("First seen", valueOr(shortStamp(d.FirstSeen), "—")),
		field("Last seen", valueOr(shortStamp(d.LastSeen), "—")),
	)
	if len(d.Occurrences) > 0 {
		lines = append(lines, field("Recent", strings.Join(occurrenceAges(d.Occurrences, m.now()), "  ")))
	}
	return lines
}

func (m Model) detailSummary(d api.IncidentDetail) string {
	switch {
	case d.ExplanationPending:
		return m.marker("analysing", "⏳ analysing")
	case d.ExplanationUnavailable:
		return m.marker("explanation unavailable", "⚠ explanation unavailable")
	default:
		return valueOr(d.Summary, "—")
	}
}

func (m Model) renderFooter() string {
	left, right := "↑/↓ select · tab panes · r refresh · ? help · q quit", ""
	if m.degraded {
		left = "up/down select | tab panes | r refresh | ? help | q quit"
	}
	switch {
	case m.conn == ConnReconnecting:
		if m.degraded {
			right = "o reconnecting"
		} else {
			right = "○ reconnecting"
		}
	default:
		if m.degraded {
			right = "* connected"
		} else {
			right = "● connected"
		}
	}
	if m.connErr != nil && m.conn == ConnReconnecting {
		right = right + ": " + m.connErr.Error()
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return m.styles.footer.Render(clip(left+strings.Repeat(" ", gap)+right, m.width))
}

func (m Model) renderHelp() string {
	lines := []string{m.styles.title.Render("watcher attach — keys"), ""}
	for _, b := range keyBindings {
		lines = append(lines, fmt.Sprintf("  %-16s %s", b.keys, b.action))
	}
	lines = append(lines, "", m.styles.dim.Render("press ? or esc to close, q to quit"))
	return strings.Join(lines, "\n")
}

func (m Model) renderTooSmall() string {
	msg := fmt.Sprintf("terminal too small: %d×%d — need at least %d×%d",
		m.width, m.height, minWidth, minHeight)
	return m.styles.title.Render(msg)
}

func (m Model) marker(ascii, unicode string) string {
	if m.degraded {
		return ascii
	}
	return unicode
}

func occurrenceAges(occurrences []string, now time.Time) []string {
	out := make([]string, 0, len(occurrences))
	for _, o := range occurrences {
		out = append(out, ageOf(o, now))
	}
	return out
}

func shortStamp(raw string) string {
	if len(raw) >= 16 {
		return raw[11:16] + "Z"
	}
	return raw
}

func field(label, value string) string {
	if label == "" {
		return strings.Repeat(" ", 13) + value
	}
	return fmt.Sprintf("%-12s %s", label, value)
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// clip truncates to width, appending an ellipsis when it had to cut. It walks
// runes so a multi-byte glyph is never split.
func clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	b.WriteString("…")
	return b.String()
}

func padRight(s string, width int) string {
	if lipgloss.Width(s) >= width {
		return clip(s, width)
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}
