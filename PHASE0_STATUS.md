## MingkunSearch Phase 0 开发进度与使用说明

更新时间：以 `memory/CURRENT_STATE.md` 的 `LastUpdated` 为准。

### 1. 当前已完成内容（Phase 0）

目标：跑通最小端到端闭环：给定研究 topic → 调研 → 生成代码 → 执行实验 → 写入 HOT Memory。

已完成：

- Go 项目初始化（`go.mod/go.sum` 已生成）
- 引入 Eino 作为编排层（`github.com/cloudwego/eino`）
- Phase 0 工作流已用 Eino `compose.Graph` 串联节点
- 文献检索：基于 arXiv API（失败会降级为提示，不阻塞流程）
- 代码生成：可选调用 OpenAI；无 Key 时使用内置 Python baseline
- 实验执行：本地 `python -c` 执行生成脚本，收集 stdout/stderr
- HOT Memory：写入 `memory/CURRENT_STATE.md`（默认截断为 2200 runes）

### 2. 代码结构（与 Phase 0 相关）

- CLI 入口：`cmd/server/main.go`
- 编排工作流（Eino Graph）：`internal/workflow/workflow.go`
- LiteratureAgent：`internal/agents/literature/literature.go`
- CodeAgent：`internal/agents/code/code.go`
- arXiv 工具：`internal/tools/arxiv/arxiv.go`
- Python 执行器：`internal/tools/python/python.go`
- HOT Memory：`internal/memory/hot/hot.go`
- OpenAI 配置：`internal/config/config.go`
- OpenAI HTTP 客户端（可选启用）：`internal/llm/openai.go`

### 3. Eino 编排说明（Phase 0）

工作流定义在 `internal/workflow/workflow.go`：

- 输入：topic（字符串）
- Eino Graph 节点：
  - `literature`：调用 `LiteratureAgent.Run`，输出调研 Markdown
  - `code`：调用 `CodeAgent.Generate`，输出 Python 代码
  - `experiment`：调用 `python.ExecCode` 执行脚本，记录 stdout/stderr
  - `hot`：将本轮状态写入 `memory/CURRENT_STATE.md`
- 输出：`workflow.Result`（literature、python、execution 聚合）

节点按顺序边连接：

`START → literature → code → experiment → hot → END`

### 4. 如何运行（最常用）

前置条件：

- Go（已在本机验证可编译运行）
- Python（需要 `python` 命令可用）

运行命令：

```powershell
cd d:\mingkunsearch
go run .\cmd\server\main.go --topic "GNN node classification on Cora dataset" --timeout 60
```

参数：

- `--topic`：研究主题
- `--timeout`：Python 执行超时（秒）
- `--hot`：HOT Memory 输出路径（默认 `memory/CURRENT_STATE.md`）

运行结果：

- 终端输出三段：
  - `=== Literature ===`：调研 Markdown
  - `=== Python ===`：生成的 Python 代码
  - `=== Execution ===`：执行输出（stdout/stderr）
- HOT Memory 写入：
  - `memory/CURRENT_STATE.md`

### 5. 如何启用 / 关闭 LLM（OpenAI）

默认行为：

- 若未设置 `OPENAI_API_KEY`：不调用 LLM，CodeAgent 走内置 baseline，LiteratureAgent 只输出 arXiv 列表。
- 若设置了 `OPENAI_API_KEY`：会调用 OpenAI Chat Completions，用于更好的调研总结与代码生成。

环境变量：

- `OPENAI_API_KEY`：必填，否则不会走 LLM
- `OPENAI_BASE_URL`：可选，默认 `https://api.openai.com`
- `OPENAI_MODEL`：可选，默认 `gpt-4o-mini`

PowerShell 示例：

```powershell
$env:OPENAI_API_KEY="sk-..."
$env:OPENAI_MODEL="gpt-4o-mini"
go run .\cmd\server\main.go --topic "graph neural networks node classification" --timeout 60
```

### 6. 常见问题

#### 6.1 arXiv 返回不相关论文

当前检索使用 `all:<topic>` 的简单策略，可能召回噪声。

临时做法：

- 在 `--topic` 中加入更明确关键词（如 `cora`, `citeseer`, `GNN`, `node classification`）。

后续优化方向（Phase 1）：

- 设计更精细的 query 规则（分字段：ti/abs/cat）
- 增加去重、排序与主题过滤

#### 6.2 go get / go mod tidy 拉取依赖失败

如果网络对默认 GOPROXY 有限制，建议在当前 PowerShell 会话设置：

```powershell
$env:GOPROXY="https://goproxy.cn,direct"
$env:GOSUMDB="sum.golang.google.cn"
go mod tidy
```

### 7. 下一阶段建议（Phase 1 起）

- 将 Phase 0 的节点升级为 Eino ADK Agent（ChatModelAgent）与 Tool 节点
- 引入 Callback 切面（OnStart/OnEnd/OnError）为 Dashboard 实时推送做准备
- 将实验执行迁移到 Docker 沙箱（对应设计文档的 sandbox 章节）
- 引入 WARM/COLD Memory（Chroma/Graphiti）

