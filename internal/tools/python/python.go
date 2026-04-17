package python

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func ExecCode(ctx context.Context, code string, timeout time.Duration) (ExecResult, error) {
	if strings.TrimSpace(code) == "" {
		return ExecResult{}, errors.New("python code is empty")
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	py := "python"
	if runtime.GOOS == "windows" {
		py = "python"
	}

	cmd := exec.CommandContext(ctx, py, "-c", code)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}

	res := ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
	return res, err
}
