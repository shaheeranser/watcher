package onboard

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Prompter reads answers from In and writes questions to Out. Both are injected
// so a test can script the input and capture the questions.
type Prompter struct {
	in  *bufio.Reader
	out io.Writer
}

// NewPrompter builds a Prompter over the given streams.
func NewPrompter(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{in: bufio.NewReader(in), out: out}
}

// Ask writes question and returns the typed answer, or def when the line is
// empty. A closed input with no answer is reported, so a loop cannot spin on a
// spent reader.
func (p *Prompter) Ask(question, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(p.out, "%s: ", question)
	}
	line, err := p.in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" && err != nil {
		return "", err
	}
	if line == "" {
		return def, nil
	}
	return line, nil
}

// Confirm asks a yes/no question. An empty answer returns def.
func (p *Prompter) Confirm(question string, def bool) (bool, error) {
	suffix := "[y/N]"
	if def {
		suffix = "[Y/n]"
	}
	fmt.Fprintf(p.out, "%s %s: ", question, suffix)
	line, err := p.in.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		if err != nil {
			return false, err
		}
		return def, nil
	}
	return line == "y" || line == "yes", nil
}
