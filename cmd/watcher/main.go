// Command watcher is the Watcher daemon: it reads one or more labeled log
// sources, notices crash and error events on its own, deduplicates them by
// fingerprint, and reports an explanation from a local model — without a human
// pointing it at an incident.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// verbs is the single registration point for subcommands. A new verb (onboard,
// eval) is added here and nowhere else; the grammar is fixed once.
var verbs = map[string]func([]string) int{
	"run":    runDaemon,
	"attach": attachCommand,
}

func main() {
	os.Exit(dispatch(os.Args[1:], verbs))
}

// dispatch interprets its first non-flag argument as a verb. A bare invocation
// (no verb, or flags first) is an alias for run, so milestone 01's invocations
// keep working unchanged.
func dispatch(args []string, table map[string]func([]string) int) int {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		verb := args[0]
		fn, ok := table[verb]
		if !ok {
			fmt.Fprintf(os.Stderr, "watcher: unknown verb %q\nvalid verbs: %s\n", verb, verbNames(table))
			return 2
		}
		return fn(args[1:])
	}
	return table["run"](args)
}

func verbNames(table map[string]func([]string) int) string {
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
