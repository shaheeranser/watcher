package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shaheeranser/watcher/internal/api"
)

// Options configures the model. Fetcher is the only required field; the rest
// have defaults so a test can build a model with one line.
type Options struct {
	Fetcher      Fetcher
	ListFraction float64
	Initial      []api.IncidentRow
	Degraded     bool
	Now          func() time.Time
}

// Model is the whole dashboard state. Rendering reads only these fields, so
// neither Update nor View touches the network or the database.
type Model struct {
	fetcher      Fetcher
	rows         []api.IncidentRow
	selected     int
	selectedID   string
	focus        Focus
	detail       *api.IncidentDetail
	detailID     string
	scroll       int
	help         bool
	conn         ConnState
	connErr      error
	err          error
	width        int
	height       int
	listFraction float64
	degraded     bool
	styles       styles
	now          func() time.Time
}

// New builds the model and sorts the initial rows.
func New(opts Options) Model {
	fraction := opts.ListFraction
	if fraction <= 0 || fraction >= 1 {
		fraction = 0.45
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	m := Model{
		fetcher:      opts.Fetcher,
		rows:         sortRows(opts.Initial),
		listFraction: fraction,
		degraded:     opts.Degraded,
		styles:       newStyles(opts.Degraded),
		now:          now,
	}
	if len(m.rows) > 0 {
		m.selectedID = m.rows[0].ID
		m.detailID = m.rows[0].ID
	}
	return m
}

// Init fetches the list and the initially selected detail.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.fetchRows()}
	if m.detailID != "" {
		cmds = append(cmds, m.fetchDetail(m.detailID))
	}
	return tea.Batch(cmds...)
}

// Update handles one message. It performs no I/O itself; the returned command
// carries any fetch off the render loop (DASH-NFR-1).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case rowsMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.rows = sortRows(msg.rows)
		return m.reselect()
	case detailMsg:
		if msg.err != nil {
			if m.detailID == msg.id {
				m.detail = nil
			}
			return m, nil
		}
		if msg.id == m.detailID {
			detail := msg.detail
			m.detail = &detail
			m.scroll = 0
		}
		return m, nil
	case eventMsg, pollMsg:
		return m, m.fetchRows()
	case connMsg:
		m.conn = msg.state
		m.connErr = msg.err
		if msg.state == ConnConnected {
			return m, tea.Batch(m.fetchRows(), m.fetchDetail(m.detailID))
		}
		return m, nil
	}
	return m, nil
}

// reselect restores the cursor after the list changes. It keeps the same
// incident selected by id where possible, so a refresh does not yank the cursor,
// and reselects the same fingerprint across a daemon restart (DASH-9).
func (m Model) reselect() (tea.Model, tea.Cmd) {
	if len(m.rows) == 0 {
		m.selected, m.selectedID, m.detailID = 0, "", ""
		m.detail = nil
		return m, nil
	}
	idx := indexOfID(m.rows, m.selectedID)
	if idx < 0 {
		idx = clamp(m.selected, 0, len(m.rows)-1)
	}
	m.selected = idx
	m.selectedID = m.rows[idx].ID
	if m.detailID != m.selectedID {
		m.detailID = m.selectedID
		m.detail = nil
		m.scroll = 0
		return m, m.fetchDetail(m.selectedID)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.help {
		switch key {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?", "esc":
			m.help = false
		}
		return m, nil
	}

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help = true
	case "up", "k":
		return m.moveTo(m.selected - 1)
	case "down", "j":
		return m.moveTo(m.selected + 1)
	case "g":
		return m.moveTo(0)
	case "G":
		return m.moveTo(len(m.rows) - 1)
	case "tab", "shift+tab":
		if m.focus == FocusList {
			m.focus = FocusDetail
		} else {
			m.focus = FocusList
		}
	case "pgup":
		m.scrollBy(-1)
	case "pgdown":
		m.scrollBy(1)
	case "r":
		return m, tea.Batch(m.fetchRows(), m.fetchDetail(m.detailID))
	}
	return m, nil
}

func (m Model) moveTo(idx int) (tea.Model, tea.Cmd) {
	if len(m.rows) == 0 {
		return m, nil
	}
	idx = clamp(idx, 0, len(m.rows)-1)
	if idx == m.selected {
		return m, nil
	}
	m.selected = idx
	m.selectedID = m.rows[idx].ID
	m.detailID = m.selectedID
	m.detail = nil
	m.scroll = 0
	return m, m.fetchDetail(m.selectedID)
}

// scrollBy moves the detail pane by a page. The view clamps the offset to the
// content, so this only needs to stay non-negative.
func (m *Model) scrollBy(pages int) {
	m.scroll += pages * m.detailPage()
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m Model) detailPage() int {
	page := m.detailHeight() - 2
	if page < 1 {
		page = 1
	}
	return page
}

func (m Model) detailHeight() int {
	listH := m.listHeight()
	detailH := m.height - 1 - listH
	if detailH < 3 {
		detailH = 3
	}
	return detailH
}

func (m Model) listHeight() int {
	listH := int(float64(m.height-1) * m.listFraction)
	if listH < 3 {
		listH = 3
	}
	if listH > m.height-4 {
		listH = m.height - 4
	}
	if listH < 1 {
		listH = 1
	}
	return listH
}

func (m Model) fetchRows() tea.Cmd {
	fetcher := m.fetcher
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		rows, err := fetcher.Incidents(ctx)
		return rowsMsg{rows: rows, err: err}
	}
}

func (m Model) fetchDetail(id string) tea.Cmd {
	if id == "" {
		return nil
	}
	fetcher := m.fetcher
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		detail, err := fetcher.Incident(ctx, id)
		return detailMsg{id: id, detail: detail, err: err}
	}
}

// pending reports that an incident's explanation has not arrived yet, and
// unavailable that the attempt failed, so the row can say so rather than show a
// blank (DASH-18).
func pending(row api.IncidentRow) bool {
	return row.ExplanationPending
}

func unavailable(row api.IncidentRow) bool {
	return row.ExplanationUnavailable
}
