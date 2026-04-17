package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/cloudwego/eino/compose"

	"mingkunsearch/internal/agents/code"
	"mingkunsearch/internal/agents/literature"
	"mingkunsearch/internal/memory/hot"
	"mingkunsearch/internal/tools/python"
)

type Result struct {
	Literature literature.Result
	Code       code.Result
	Exec       python.ExecResult
}

type Workflow struct {
	LiteratureAgent literature.Agent
	CodeAgent       code.Agent
	HotMemory       hot.Store
	PythonTimeout   time.Duration
}

func (w Workflow) Run(ctx context.Context, topic string) (Result, error) {
	g := compose.NewGraph[*graphState, *graphState]()

	if err := g.AddLambdaNode("literature", compose.InvokableLambda(func(ctx context.Context, s *graphState) (*graphState, error) {
		if s == nil {
			return nil, errors.New("nil state")
		}
		lit, err := w.LiteratureAgent.Run(ctx, s.Topic, 5)
		if err != nil {
			return nil, err
		}
		s.Literature = lit
		return s, nil
	})); err != nil {
		return Result{}, err
	}

	if err := g.AddLambdaNode("code", compose.InvokableLambda(func(ctx context.Context, s *graphState) (*graphState, error) {
		if s == nil {
			return nil, errors.New("nil state")
		}
		py, err := w.CodeAgent.Generate(ctx, code.Input{
			Topic:        s.Topic,
			LiteratureMD: s.Literature.MD,
		})
		if err != nil {
			return nil, err
		}
		s.Code = py
		return s, nil
	})); err != nil {
		return Result{}, err
	}

	if err := g.AddLambdaNode("experiment", compose.InvokableLambda(func(ctx context.Context, s *graphState) (*graphState, error) {
		if s == nil {
			return nil, errors.New("nil state")
		}
		execRes, execErr := python.ExecCode(ctx, s.Code.Python, w.PythonTimeout)
		s.Exec = execRes
		s.ExecErr = execErr
		return s, nil
	})); err != nil {
		return Result{}, err
	}

	if err := g.AddLambdaNode("hot", compose.InvokableLambda(func(ctx context.Context, s *graphState) (*graphState, error) {
		if s == nil {
			return nil, errors.New("nil state")
		}
		_ = w.HotMemory.Write(hot.State{
			Topic:        s.Topic,
			LiteratureMD: s.Literature.MD,
			CodePython:   s.Code.Python,
			ExecStdout:   s.Exec.Stdout,
			ExecStderr:   s.Exec.Stderr,
		})
		return s, nil
	})); err != nil {
		return Result{}, err
	}

	_ = g.AddEdge(compose.START, "literature")
	_ = g.AddEdge("literature", "code")
	_ = g.AddEdge("code", "experiment")
	_ = g.AddEdge("experiment", "hot")
	_ = g.AddEdge("hot", compose.END)

	runnable, err := g.Compile(ctx, compose.WithGraphName("phase0"))
	if err != nil {
		return Result{}, err
	}
	out, err := runnable.Invoke(ctx, &graphState{Topic: topic})
	if err != nil {
		return Result{}, err
	}
	res := Result{Literature: out.Literature, Code: out.Code, Exec: out.Exec}
	if out.ExecErr != nil {
		return res, out.ExecErr
	}
	return res, nil
}

type graphState struct {
	Topic      string
	Literature literature.Result
	Code       code.Result
	Exec       python.ExecResult
	ExecErr    error
}
