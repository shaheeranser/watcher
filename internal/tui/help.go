package tui

// binding is one row of the help overlay.
type binding struct {
	keys   string
	action string
}

// keyBindings mirrors the design's key table (DASH-14).
var keyBindings = []binding{
	{"↑ / k", "move selection up"},
	{"↓ / j", "move selection down"},
	{"g / G", "jump to first / last row"},
	{"tab / shift+tab", "switch focus between list and detail"},
	{"pgup / pgdown", "scroll the detail pane"},
	{"r", "force refresh"},
	{"?", "toggle this help"},
	{"q / ctrl+c", "quit attach (the daemon keeps running)"},
}
