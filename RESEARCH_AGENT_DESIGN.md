# MingkunSearch — 自主科研 Agent 最终技术方案

> 目标：给定一个研究方向，Agent 能自主完成文献调研、实验设计、代码实现、结果验证，并生成论文草稿。
> 
> 工作目录：`D:\mingkunsearch`  
> 可视化界面：自研 Web Dashboard（前后端分离）
> 
> Agent 框架：**Eino**（字节跳动 CloudWeGo，Go 语言）
> 文档版本：v4.0 | 最后更新：2026-04-17

---

## 一、整体架构

```
┌──────────────────────────────────────────────────────────┐
│                   自研 Web Dashboard                     │
│     (React + ReactFlow + Recharts + WebSocket)           │
└───────────────────────────┬──────────────────────────────┘
                            │ REST API + WebSocket
┌───────────────────────────▼──────────────────────────────┐
│                  Eino DeepAgent（总调度）                  │
│   Graph 编排 + SubAgent 委托 + BFTS 树搜索               │
│   Callback 切面 → 实时推送 Agent 状态到 Dashboard         │
└──┬──────────┬──────────┬───────────┬──────────┬──────────┘
   │          │          │           │          │
   ▼          ▼          ▼           ▼          ▼
LitAgent  HypAgent  CodeAgent   ExpAgent  PaperAgent
(调研)    (假设)    (代码生成)   (实验)    (论文撰写)
   │          │          │           │          │
   └──────────┴──────────┴─────┬─────┘          │
                               ▼                │
                  ┌─────────────────────┐        │
                  │    三层记忆系统      │◄───────┘
                  │  HOT / WARM / COLD  │
                  └─────────────────────┘
                               │
                    ┌──────────┴──────────┐
                    ▼                     ▼
              Docker 沙箱              MLflow
           (Python 实验执行)         (指标追踪)
```

---

## 二、六大子 Agent 职责

| Agent | Eino 实现方式 | 核心职责 | 主要工具（Eino Tool） |
|-------|-------------|----------|---------------------|
| **CodeComprehensionAgent** 🆕 | `ChatModelAgent` + 图查询 Tool | 理解已有代码库，梳理研究进展 | SearchSymbol、TraceCallChain、ListModels、ListResults |
| **LiteratureAgent** | `ChatModelAgent` + 自定义 Tool | 文献检索、综述生成、Gap 分析 | arXivAPI、SemanticScholarTool、WebFetch |
| **HypothesisAgent** | `ChatModelAgent` + 记忆检索 Tool | 基于文献+已有代码生成实验假设 | MemoryRetriever（读 WARM/COLD）+ 代码图谱查询 |
| **CodeAgent** | `ChatModelAgent` + ShellTool | **基于已有代码修改/扩展**（非从零生成） | ShellTool、ModifyCodeTool、TraceCallChainTool |
| **ExpAgent** | `ChatModelAgent` + DockerTool | 执行实验、监控进度、收集结果 | DockerExecTool、MLflowLogTool |
| **PaperAgent** | `ChatModelAgent` + 文件 Tool | 结果分析、图表生成、论文草稿 | FileWriteTool、LatexTool |

> **详细设计**：代码理解模块见 [`CODE_COMPREHENSION_DESIGN.md`](./CODE_COMPREHENSION_DESIGN.md)

**编排方式：** DeepAgent 作为主 Agent，通过 `SetSubAgents()` 注册 6 个子 Agent。当用户提供代码路径时，先触发 CodeComprehensionAgent 理解代码，再转入 Lit→Hyp→Code→Exp→Paper 正常工作流。CodeAgent 增强为可查询代码图谱、精确定位修改。

---

## 三、Eino 框架核心能力映射

### 3.1 编排层

```go
// 主编排图：研究工作流
graph := compose.NewGraph[*ResearchState, *ResearchResult]()

// 添加各 Agent 节点
graph.AddChatModelNode("literature", litAgent.ChatModel)
graph.AddChatModelNode("hypothesis", hypAgent.ChatModel)
graph.AddChatModelNode("code", codeAgent.ChatModel)
graph.AddChatModelNode("experiment", expAgent.ChatModel)
graph.AddChatModelNode("paper", paperAgent.ChatModel)

// 定义流转边
graph.AddEdge(compose.START, "literature")
graph.AddEdge("literature", "hypothesis")
graph.AddBranch("hypothesis", hypothesisBranch)  // 根据假设数量分支
graph.AddEdge("code", "experiment")
graph.AddBranch("experiment", evalBranch)         // 结果达标才写论文
graph.AddEdge("experiment", "paper")              // 或反馈回 hypothesis
graph.AddEdge("paper", compose.END)
```

### 3.2 DeepAgent 多 Agent 协作

```go
// DeepAgent 作为总调度
deepAgent, _ := deep.New(ctx, &deep.Config{
    ChatModel: chatModel,
    SubAgents: []adk.Agent{litAgent, hypAgent, codeAgent, expAgent, paperAgent},
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: []tool.BaseTool{
                memoryRetrieverTool,  // 记忆检索
                dockerTool,           // 实验沙箱
                mlflowTool,           // 指标记录
                webSearchTool,        // 文献检索
            },
        },
    },
})
```

### 3.3 Callback → Dashboard 实时推送

```go
// Eino 原生 Callback 机制，天然适配 WebSocket 推送
graph.AddCallbacks(&compose.CallbackConfig{
    OnStart: func(ctx context.Context, info *compose.RunInfo, input compose.Input) error {
        dashboard.PushEvent(ctx, DashboardEvent{
            Agent:    info.NodeName,
            Status:   "running",
            Message:  "开始执行...",
            Time:     time.Now(),
        })
        return nil
    },
    OnEnd: func(ctx context.Context, info *compose.RunInfo, output compose.Output) error {
        dashboard.PushEvent(ctx, DashboardEvent{
            Agent:    info.NodeName,
            Status:   "completed",
            Time:     time.Now(),
        })
        return nil
    },
    OnError: func(ctx context.Context, info *compose.RunInfo, err error) error {
        dashboard.PushEvent(ctx, DashboardEvent{
            Agent:    info.NodeName,
            Status:   "error",
            Message:  err.Error(),
            Time:     time.Now(),
        })
        return nil
    },
})
```

### 3.4 人机协作（中断/恢复）

```go
// 实验前等待人工确认
func experimentGuard(ctx context.Context, plan *ExperimentPlan) (*ExperimentPlan, error) {
    // 弹出确认请求到 Dashboard
    return adk.Interrupt(ctx, &HumanConfirm{
        Message: "即将执行实验，请确认以下参数：",
        Plan:    plan,
    })
}

// 从检查点恢复
iter, _ := runner.Resume(ctx, checkpointID)
```

---

## 四、三层记忆系统（最核心）

```
┌─────────────────────────────────────────┐
│  HOT Memory（热记忆）                    │
│  文件：CURRENT_STATE.md ≤ 2200 字        │
│  内容：当前研究问题、进行中实验、最新结论  │
│  读写：每次 Agent 调用都注入 ChatTemplate │
│  实现：Go 直接读写文件                    │
├─────────────────────────────────────────┤
│  WARM Memory（温记忆）                   │
│  存储：Chroma 向量数据库（HTTP API）      │
│  内容：历史实验记录、文献摘要、代码片段   │
│  检索：Eino Retriever 组件封装           │
│  实现：Go HTTP 调用 Chroma Server        │
├─────────────────────────────────────────┤
│  COLD Memory（冷记忆）                   │
│  存储：Graphiti 时态知识图谱             │
│  内容：方法对比关系、时间维度实验结论     │
│  示例："2025-03 GraphSage 超越 GAT"      │
│  优势：能回答"哪个方法现在最 SOTA"       │
│  实现：Go gRPC 调用 Graphiti Server      │
└─────────────────────────────────────────┘
```

### Eino Retriever 封装

```go
// 将 Chroma 封装为 Eino Retriever 组件
type MemoryRetriever struct {
    chromaClient *chromago.Client
    collection   string
}

func (r *MemoryRetriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
    // 调用 Chroma HTTP API 查询
    results, err := r.chromaClient.QueryCollection(ctx, r.collection, query, 10)
    // 转换为 Eino Document 格式
    return toEinoDocuments(results), nil
}

// 注册为 Eino Tool，供 HypothesisAgent 等调用
memoryTool := tool.NewTool(
    &MemoryRetriever{},
    tool.WithName("memory_search"),
    tool.WithDescription("搜索历史实验记录和研究结论"),
)
```

**为什么不用纯 RAG？**  
纯向量库无法回答时序性问题（"3 个月前 A 超 B，现在 C 又超 A"）；Graphiti 知识图谱有时间戳，实验结论会随时间迭代更新。

---

## 五、实验探索策略：BFTS 树搜索

借鉴 AI Scientist v2，利用 Go 天然并发优势：

```go
// BFTS 节点
type ExperimentNode struct {
    ID        string
    Hypothesis string
    Code      string
    Status    NodeStatus  // Pending / Running / Success / Failed / Pruned
    Retry     int         // 已重试次数
    Results   []Metric    // 多种子实验结果
    Children  []*ExperimentNode
}

// BFTS 并行展开（goroutine 天然并发）
func (b *BFTS) Expand(ctx context.Context, node *ExperimentNode) error {
    candidates := b.GenerateVariants(node)  // 生成 3 个变体
    
    var wg sync.WaitGroup
    for _, child := range candidates {
        wg.Add(1)
        go func(n *ExperimentNode) {
            defer wg.Done()
            for retry := 0; retry < 3; retry++ {
                result, err := b.Execute(ctx, n)  // Docker 执行
                if err == nil && b.IsSignificant(result) {
                    n.Status = Success
                    break
                }
                b.Debug(ctx, n, err)  // 自动调试
            }
        }(child)
    }
    wg.Wait()
    
    // 只保留成功节点，剪枝失败节点
    b.PruneFailed(candidates)
    return nil
}
```

```
                [根节点：研究假设]
               /        |        \
        [方案A]       [方案B]    [方案C]   ← goroutine 并行
        /    \           |
   [A-变种1] [A-变种2]  [B-调参]
```

- **并行度**：默认 3 个分支同时运行（Go goroutine）
- **失败处理**：代码/环境报错自动重试 ≤ 3 次，超出则剪枝
- **节点评分**：实验结果达标（p<0.05 + 多种子验证）才扩展子节点
- **结果写入记忆**：只有统计显著的结论才更新 COLD Memory

---

## 六、沙箱隔离

所有代码实验在 Docker 容器中执行，Go 通过 `docker-py` 或 Docker Engine API 调用：

```yaml
# docker-compose.yml
services:
  sandbox:
    image: mingkun/research-sandbox:latest  # Python + PyTorch + sklearn
    volumes:
      - ./experiments:/workspace
      - ./data:/data:ro
    deploy:
      resources:
        limits:
          memory: 8G
    network_mode: none  # 默认隔离网络
```

```go
// Go 调用 Docker API
func ExecuteInSandbox(ctx context.Context, code string, timeout time.Duration) (*ExecResult, error) {
    cli, _ := client.NewClientWithOpts(client.FromEnv)
    resp, err := cli.ContainerCreate(ctx, &container.Config{
        Image: "mingkun/research-sandbox:latest",
        Cmd:   []string{"python", "-c", code},
    }, &container.HostConfig{
        NetworkMode: "none",
        AutoRemove:  true,
    }, nil, nil, "")
    // ... 启动、等待、收集输出
}
```

- 实验产物（模型权重、日志、图表）挂载到宿主机 `./experiments/`
- MLflow 追踪每次实验的超参数和指标

---

## 七、自研可视化 Dashboard 设计

**技术栈：**
- 后端：**Go (net/http + gorilla/websocket)** — 与 Agent 同进程，零延迟
- 前端：React + ReactFlow（节点连线图）+ Recharts（实验曲线）
- 数据库：SQLite（任务状态，Go database/sql 原生支持）

> 后端用 Go 而非 FastAPI，和 Agent 同语言、同进程，Eino Callback 直接推送，无跨进程开销。

**核心页面：**

```
├── /dashboard     主控台（当前研究进度、HOT Memory 摘要）
├── /workflow      Agent 工作流图（ReactFlow 节点连线，实时高亮）
├── /experiments   实验列表（BFTS 树形展示 + MLflow 指标）
├── /memory        记忆浏览器（HOT/WARM/COLD 三层查看）
└── /paper         论文草稿预览（Markdown + LaTeX 渲染）
```

**WebSocket 推送（Go 原生）：**
```go
func (h *Hub) PushAgentStatus(agent, status, message string) {
    event := AgentEvent{
        Event:     "agent_status",
        Agent:     agent,
        Status:    status,
        Message:   message,
        Timestamp: time.Now().Format(time.RFC3339),
    }
    data, _ := json.Marshal(event)
    for _, conn := range h.connections {
        conn.WriteMessage(websocket.TextMessage, data)
    }
}
```

---

## 八、开发路线图（6 阶段）

### Phase 0：基础设施（第 1 周）

**目标**：Go 项目搭建 + 最小 Demo 跑通

- [ ] `go mod init mingkunsearch`，搭建项目结构
- [ ] 安装 Eino：`go get github.com/cloudwego/eino/...`
- [ ] 配置 LLM（先用 GPT-4o，Eino 原生支持 OpenAI）
- [ ] 实现 HOT Memory（Go 读写 `CURRENT_STATE.md`）
- [ ] 实现最简 LiteratureAgent（arXiv Tool + ChatModelAgent）
- [ ] 实现最简 CodeAgent（ShellTool 调用 Python）
- [ ] 跑通：给定 "GNN 节点分类" → 搜文献 → 生成代码 → 执行

**交付物**：CLI 端到端 Demo 跑通

---

### Phase 1：Agent 完善（第 2-3 周）

**目标**：5 个子 Agent 全部实现，DeepAgent 编排稳定

- [ ] 完善 LiteratureAgent（加 Semantic Scholar Tool、文献去重）
- [ ] 实现 HypothesisAgent（综述 → 3-5 个实验假设）
- [ ] 完善 CodeAgent（DockerTool 调用沙箱、调试重试）
- [ ] 实现 ExpAgent（Docker 执行 + MLflow 日志）
- [ ] 实现 PaperAgent（结果 → 图表 → Markdown 草稿）
- [ ] DeepAgent 编排：SubAgent 注册 + Graph 流转

**交付物**：完整 5-Agent 工作流跑通

---

### Phase 2：记忆系统（第 4 周）

**目标**：三层记忆上线

- [ ] WARM Memory：Chroma Server 部署 + Eino Retriever 封装
- [ ] COLD Memory：Graphiti Server 部署 + Go gRPC 客户端
- [ ] HOT Memory 自动更新（每轮实验后刷新）
- [ ] MemoryRetriever Tool（统一检索接口）
- [ ] 测试：第二轮实验能检索到第一轮的结论

**交付物**：记忆系统测试通过

---

### Phase 3：BFTS 树搜索（第 5-6 周）

**目标**：从顺序执行升级为并行树搜索

- [ ] ExperimentNode 数据结构 + 持久化（SQLite）
- [ ] BFTS Expand 并行展开（goroutine + errgroup）
- [ ] 失败自动重试 + 剪枝逻辑
- [ ] 多种子统计显著性验证（≥3 seed，p<0.05）
- [ ] 树搜索结果合并写入记忆

**交付物**：同一假设能并行探索 3 种实现方案

---

### Phase 4：可视化 Dashboard（第 7-9 周）

**目标**：自研 Web UI

- [ ] Go HTTP Server + WebSocket Hub（与 Agent 同进程）
- [ ] Eino Callback → WebSocket 实时推送
- [ ] React 前端：ReactFlow 节点状态图
- [ ] `/experiments` 页：BFTS 树形展示 + Recharts 曲线
- [ ] `/memory` 页：三层记忆浏览器
- [ ] `/paper` 页：Markdown 渲染 + 人工干预

**交付物**：完整 Web Dashboard 可用

---

### Phase 5：系统优化与评测（第 10-12 周）

**目标**：生产可用

- [ ] 成本控制（LLM token 预算、Qwen/Ollama 降级）
- [ ] 错误恢复（Agent 崩溃重启 + 检查点恢复）
- [ ] 本地模型支持（Eino 原生支持 Ollama）
- [ ] 真实课题完整跑一次（建议：GNN + Cora/Citeseer）
- [ ] 输出论文草稿
- [ ] 性能评估：文献覆盖率、实验成功率、草稿质量

**交付物**：评测报告 + 论文草稿

---

## 九、技术栈总览

```
Agent 框架：   Eino v0.8+（字节跳动 CloudWeGo，Go 语言）
编排：         Eino Graph + DeepAgent + SubAgent 委托
LLM：          GPT-4o（开发期） → Ollama/Qwen2.5（降本，Eino 原生支持）
向量数据库：   Chroma（HTTP API，Go 客户端调用）
知识图谱：     Graphiti + Neo4j（gRPC/REST API）
实验追踪：     MLflow（Python 服务，Go HTTP 调用）
沙箱执行：     Docker Engine API（Go SDK）
文献检索：     Semantic Scholar API、arXiv API
前端：         React + ReactFlow + Recharts
后端 HTTP：    Go net/http + gorilla/websocket（与 Agent 同进程）
持久化：       SQLite（Go database/sql）+ 文件系统（HOT Memory）
```

---

## 十、项目目录结构

```
D:\mingkunsearch\
├── cmd/
│   └── server/
│       └── main.go                    # 入口：启动 Agent + Dashboard
├── internal/
│   ├── agents/
│   │   ├── literature.go              # LiteratureAgent
│   │   ├── hypothesis.go              # HypothesisAgent
│   │   ├── code.go                    # CodeAgent
│   │   ├── experiment.go              # ExpAgent
│   │   ├── paper.go                   # PaperAgent
│   │   └── deepagent.go               # DeepAgent 总调度
│   ├── memory/
│   │   ├── hot.go                     # HOT Memory 读写
│   │   ├── warm.go                    # Chroma WARM Retriever
│   │   ├── cold.go                    # Graphiti COLD Retriever
│   │   └── retriever.go               # 统一记忆检索 Tool
│   ├── bfts/
│   │   ├── node.go                    # 实验节点
│   │   ├── tree.go                    # BFTS 树搜索
│   │   └── evaluator.go               # 统计显著性评估
│   ├── tools/
│   │   ├── arxiv.go                   # arXiv API Tool
│   │   ├── semantic_scholar.go        # Semantic Scholar Tool
│   │   ├── docker.go                  # Docker 沙箱 Tool
│   │   ├── mlflow.go                  # MLflow 日志 Tool
│   │   ├── shell.go                   # Shell 执行 Tool
│   │   └── file.go                    # 文件读写 Tool
│   ├── dashboard/
│   │   ├── handler.go                 # HTTP Handler
│   │   ├── websocket.go               # WebSocket Hub
│   │   └── callback.go                # Eino Callback → WS 推送
│   └── graph/
│       └── workflow.go                # Eino Graph 编排定义
├── web/                                # React 前端
│   ├── package.json
│   ├── src/
│   │   ├── App.tsx
│   │   ├── pages/
│   │   ├── components/
│   │   └── hooks/
│   └── vite.config.ts
├── sandbox/
│   ├── docker-compose.yml
│   └── Dockerfile                     # Python + PyTorch + ML 实验
├── memory/
│   ├── CURRENT_STATE.md               # HOT Memory
│   └── chroma_data/                   # Chroma 持久化
├── experiments/                        # 实验产物
├── data/                               # 数据集
├── go.mod
├── go.sum
├── Makefile
└── RESEARCH_AGENT_DESIGN.md            # 本文档
```

---

## 十一、Phase 0 快速启动

```bash
# 1. 初始化 Go 项目
cd D:\mingkunsearch
go mod init mingkunsearch

# 2. 安装 Eino
go get github.com/cloudwego/eino@latest
go get github.com/cloudwego/eino/components/...
go get github.com/cloudwego/eino/ext/components/model/openai@latest

# 3. 启动 Chroma（WARM Memory）
docker run -d -p 8000:8000 chromadb/chroma:latest

# 4. 设置环境变量
export OPENAI_API_KEY="sk-xxx"

# 5. 跑最小 Demo
go run cmd/server/main.go --topic "GNN node classification on Cora dataset"
```

---

## 十二、Eino 选型优势总结

| 优势 | 具体表现 |
|------|---------|
| **Go 性能** | 编译型语言，天然并发（goroutine），BFTS 并行搜索零开销 |
| **同语言全栈** | Agent + 后端 HTTP + WebSocket 全 Go，无跨进程通信成本 |
| **Callback 机制** | 原生 OnStart/OnEnd/OnError 切面，直接推送到 Dashboard |
| **Graph 编排** | 编译时类型检查，流处理自动化，比 Python 框架更可靠 |
| **人机协作** | 原生中断/恢复/检查点，科研实验的人工确认非常自然 |
| **Tool 系统** | Agent 封装为 Tool，嵌套调用；Docker/Shell 调用 Python 实验 |
| **本地模型** | 原生支持 Ollama，降本路线清晰 |
| **中文文档** | CloudWeGo 官方文档完善，社区活跃（10.7k⭐） |

---

*文档版本：v4.0 | 最后更新：2026-04-17 | 框架：Eino (Go)*

> v4.0 更新：新增 **CodeComprehensionAgent**（基于 Tree-Sitter + 代码知识图谱的代码理解能力），支持用户传入已有代码库继续研究。详见 [CODE_COMPREHENSION_DESIGN.md](./CODE_COMPREHENSION_DESIGN.md)。
