package literature

import (
	"context"
	"fmt"
	"strings"

	"mingkunsearch/internal/llm"
	"mingkunsearch/internal/tools/arxiv"
)

type Result struct {
	Papers []arxiv.Paper
	MD     string
}

type Agent struct {
	Arxiv *arxiv.Client
	LLM   llm.Client
}

func (a Agent) Run(ctx context.Context, topic string, maxPapers int) (Result, error) {
	ax := a.Arxiv
	if ax == nil {
		ax = &arxiv.Client{}
	}
	papers, err := ax.Search(ctx, topic, maxPapers)
	if err != nil {
		return Result{
			Papers: nil,
			MD:     fmt.Sprintf("- arXiv search failed: %s\n", err.Error()),
		}, nil
	}

	md := a.renderBasicMarkdown(papers)
	if a.LLM != nil && len(papers) > 0 {
		llmMD, err := a.summarizeWithLLM(ctx, topic, papers)
		if err == nil && strings.TrimSpace(llmMD) != "" {
			md = llmMD
		}
	}
	return Result{Papers: papers, MD: md}, nil
}

func (a Agent) renderBasicMarkdown(papers []arxiv.Paper) string {
	if len(papers) == 0 {
		return "- No papers found from arXiv.\n"
	}
	var b strings.Builder
	for i, p := range papers {
		b.WriteString(fmt.Sprintf("- [%d] %s", i+1, p.Title))
		if p.PDFURL != "" {
			b.WriteString(" (")
			b.WriteString(p.PDFURL)
			b.WriteString(")")
		}
		b.WriteString("\n")
		if p.Summary != "" {
			b.WriteString("  - ")
			b.WriteString(trunc(p.Summary, 240))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (a Agent) summarizeWithLLM(ctx context.Context, topic string, papers []arxiv.Paper) (string, error) {
	var b strings.Builder
	for i, p := range papers {
		b.WriteString(fmt.Sprintf("[%d] Title: %s\n", i+1, p.Title))
		if len(p.Authors) > 0 {
			b.WriteString("Authors: ")
			b.WriteString(strings.Join(p.Authors, ", "))
			b.WriteString("\n")
		}
		if p.Summary != "" {
			b.WriteString("Abstract: ")
			b.WriteString(trunc(p.Summary, 900))
			b.WriteString("\n")
		}
		if p.PDFURL != "" {
			b.WriteString("PDF: ")
			b.WriteString(p.PDFURL)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	system := "You are a research assistant. Return concise markdown. Do not include code blocks."
	user := fmt.Sprintf(
		"Topic: %s\n\nPapers:\n%s\nTask:\n- Select up to 5 most relevant papers\n- Summarize key ideas (2-3 bullets each)\n- Identify gaps and propose 2 experiment hypotheses\nOutput markdown only.",
		topic,
		b.String(),
	)
	return a.LLM.Chat(ctx, system, user)
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
