package hot

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

type State struct {
	Topic        string
	LiteratureMD string
	CodePython   string
	ExecStdout   string
	ExecStderr   string
}

type Store struct {
	Path      string
	MaxRunes  int
	TimeNowFn func() time.Time
}

func (s Store) Write(state State) error {
	path := s.Path
	if strings.TrimSpace(path) == "" {
		path = filepath.FromSlash("memory/CURRENT_STATE.md")
	}
	maxRunes := s.MaxRunes
	if maxRunes <= 0 {
		maxRunes = 2200
	}
	nowFn := s.TimeNowFn
	if nowFn == nil {
		nowFn = time.Now
	}

	content := renderMarkdown(state, nowFn())
	content = truncateRunes(content, maxRunes)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func renderMarkdown(s State, now time.Time) string {
	var b strings.Builder
	b.WriteString("# CURRENT_STATE\n\n")
	b.WriteString("LastUpdated: ")
	b.WriteString(now.Format(time.RFC3339))
	b.WriteString("\n\n")

	if strings.TrimSpace(s.Topic) != "" {
		b.WriteString("## Topic\n\n")
		b.WriteString(s.Topic)
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(s.LiteratureMD) != "" {
		b.WriteString("## Literature\n\n")
		b.WriteString(s.LiteratureMD)
		if !strings.HasSuffix(s.LiteratureMD, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if strings.TrimSpace(s.CodePython) != "" {
		b.WriteString("## Code\n\n```python\n")
		b.WriteString(s.CodePython)
		if !strings.HasSuffix(s.CodePython, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n\n")
	}
	if strings.TrimSpace(s.ExecStdout) != "" || strings.TrimSpace(s.ExecStderr) != "" {
		b.WriteString("## Experiment\n\n")
		if strings.TrimSpace(s.ExecStdout) != "" {
			b.WriteString("### Stdout\n\n```text\n")
			b.WriteString(s.ExecStdout)
			if !strings.HasSuffix(s.ExecStdout, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n\n")
		}
		if strings.TrimSpace(s.ExecStderr) != "" {
			b.WriteString("### Stderr\n\n```text\n")
			b.WriteString(s.ExecStderr)
			if !strings.HasSuffix(s.ExecStderr, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n\n")
		}
	}
	return b.String()
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	return string(rs[:max])
}
