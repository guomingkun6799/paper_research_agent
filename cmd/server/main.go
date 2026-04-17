package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"mingkunsearch/internal/agents/code"
	"mingkunsearch/internal/agents/literature"
	"mingkunsearch/internal/config"
	"mingkunsearch/internal/llm"
	"mingkunsearch/internal/memory/hot"
	"mingkunsearch/internal/tools/arxiv"
	"mingkunsearch/internal/workflow"
)

func main() {
	var topic string
	var hotPath string
	var timeoutSec int
	flag.StringVar(&topic, "topic", "GNN node classification on Cora dataset", "research topic")
	flag.StringVar(&hotPath, "hot", "memory/CURRENT_STATE.md", "HOT memory markdown path")
	flag.IntVar(&timeoutSec, "timeout", 120, "python timeout in seconds")
	flag.Parse()

	topic = strings.TrimSpace(topic)
	if topic == "" {
		fmt.Fprintln(os.Stderr, "topic is empty")
		os.Exit(2)
	}

	cfg := config.Load()
	var client llm.Client
	if strings.TrimSpace(cfg.OpenAIAPIKey) != "" {
		client = &llm.OpenAIClient{
			APIKey:  cfg.OpenAIAPIKey,
			BaseURL: cfg.OpenAIBaseURL,
			Model:   cfg.OpenAIModel,
		}
	}

	litAgent := literature.Agent{
		Arxiv: &arxiv.Client{},
		LLM:   client,
	}
	codeAgent := code.Agent{LLM: client}
	wf := workflow.Workflow{
		LiteratureAgent: litAgent,
		CodeAgent:       codeAgent,
		HotMemory:       hot.Store{Path: hotPath},
		PythonTimeout:   time.Duration(timeoutSec) * time.Second,
	}

	ctx := context.Background()
	res, err := wf.Run(ctx, topic)

	fmt.Println("=== Literature ===")
	fmt.Println(res.Literature.MD)
	fmt.Println("=== Python ===")
	fmt.Println(res.Code.Python)
	fmt.Println("=== Execution ===")
	if strings.TrimSpace(res.Exec.Stdout) != "" {
		fmt.Println(res.Exec.Stdout)
	}
	if strings.TrimSpace(res.Exec.Stderr) != "" {
		fmt.Fprintln(os.Stderr, res.Exec.Stderr)
	}
	fmt.Println("HOT written to:", hotPath)

	if err != nil {
		fmt.Fprintln(os.Stderr, "run error:", err)
		os.Exit(1)
	}
}
