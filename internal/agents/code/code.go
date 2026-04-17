package code

import (
	"context"
	"fmt"
	"strings"

	"mingkunsearch/internal/llm"
)

type Input struct {
	Topic        string
	LiteratureMD string
}

type Result struct {
	Python string
}

type Agent struct {
	LLM llm.Client
}

func (a Agent) Generate(ctx context.Context, in Input) (Result, error) {
	if a.LLM == nil {
		return Result{Python: fallbackPython(in)}, nil
	}
	system := "You generate runnable Python code. Output code only. Use only Python standard library. No network access. Keep runtime under 10 seconds."
	user := fmt.Sprintf(
		"Topic: %s\n\nLiterature notes (markdown):\n%s\n\nTask: write a small self-contained experiment script.\nRequirements:\n- print topic\n- create a synthetic classification dataset\n- train a simple model (logistic regression from scratch)\n- print accuracy\n- print a short conclusion line\nOutput code only.",
		in.Topic,
		in.LiteratureMD,
	)
	out, err := a.LLM.Chat(ctx, system, user)
	if err != nil {
		return Result{Python: fallbackPython(in)}, nil
	}
	code := strings.TrimSpace(out)
	code = stripCodeFences(code)
	if code == "" {
		return Result{Python: fallbackPython(in)}, nil
	}
	return Result{Python: code}, nil
}

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSpace(s)
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			lang := strings.TrimSpace(s[:i])
			_ = lang
			s = s[i+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	return strings.TrimSpace(s)
}

func fallbackPython(in Input) string {
	topic := strings.ReplaceAll(in.Topic, "\\", "\\\\")
	topic = strings.ReplaceAll(topic, "\"", "\\\"")

	return fmt.Sprintf(`import math
import random

random.seed(0)

TOPIC = "%s"

def make_data(n=400, d=6):
    X = []
    y = []
    w = [random.uniform(-1, 1) for _ in range(d)]
    b = random.uniform(-0.2, 0.2)
    for _ in range(n):
        x = [random.gauss(0, 1) for _ in range(d)]
        z = sum(wi*xi for wi, xi in zip(w, x)) + b + random.gauss(0, 0.5)
        p = 1.0 / (1.0 + math.exp(-z))
        label = 1 if p > 0.5 else 0
        if random.random() < 0.05:
            label = 1 - label
        X.append(x)
        y.append(label)
    return X, y

def sigmoid(z):
    if z >= 0:
        ez = math.exp(-z)
        return 1.0 / (1.0 + ez)
    ez = math.exp(z)
    return ez / (1.0 + ez)

def train_logreg(X, y, lr=0.2, steps=600, l2=1e-3):
    d = len(X[0])
    w = [0.0]*d
    b = 0.0
    n = len(X)
    for _ in range(steps):
        gw = [0.0]*d
        gb = 0.0
        for xi, yi in zip(X, y):
            z = sum(wj*xj for wj, xj in zip(w, xi)) + b
            pi = sigmoid(z)
            diff = (pi - yi)
            for j in range(d):
                gw[j] += diff * xi[j]
            gb += diff
        for j in range(d):
            gw[j] = gw[j]/n + l2*w[j]
            w[j] -= lr*gw[j]
        b -= lr*(gb/n)
    return w, b

def predict(w, b, X):
    out = []
    for xi in X:
        z = sum(wj*xj for wj, xj in zip(w, xi)) + b
        out.append(1 if sigmoid(z) >= 0.5 else 0)
    return out

def accuracy(y_true, y_pred):
    c = sum(1 for a, b in zip(y_true, y_pred) if a == b)
    return c / max(1, len(y_true))

def main():
    X, y = make_data()
    split = int(0.8*len(X))
    Xtr, ytr = X[:split], y[:split]
    Xte, yte = X[split:], y[split:]
    w, b = train_logreg(Xtr, ytr)
    pred = predict(w, b, Xte)
    acc = accuracy(yte, pred)
    print("topic:", TOPIC)
    print("n_train:", len(Xtr), "n_test:", len(Xte))
    print("accuracy:", round(acc, 4))
    if acc >= 0.75:
        print("conclusion: the toy baseline is strong; next step is to replace synthetic data with a real benchmark.")
    else:
        print("conclusion: baseline underperforms; next step is to tune optimization and feature scale.")

if __name__ == "__main__":
    main()
` , topic)
}
