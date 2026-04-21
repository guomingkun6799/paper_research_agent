# Agent Dashboard 可视化方案设计

> 文档版本：v1.0 | 参考：Multica 可视化设计 | 技术栈：Go + Eino + React + ReactFlow

---

## 一、目标效果（对标 Multica）

| 功能 | Multica 的做法 | 我们的做法 |
|------|--------------|-----------|
| 多 Agent 状态总览 | 侧边栏列表 + 状态指示器 | ReactFlow 节点图，每个 Agent 一个节点，颜色代表状态 |
| 任务流程 | 看板（Kanban） | 有向图，边代表消息流向（A→B→C） |
| 单 Agent 日志 | 侧边抽屉 + Tasks Tab | 点击节点弹出详情面板，流式显示 LLM 输出 |
| 实时更新 | WebSocket | 同（gorilla/websocket，Go 内置） |
| Token 用量 | 用量报告 | Recharts 折线图，实时累加 |
| 执行历史 | 任务历史列表 | 时间轴视图，可回放 |

---

## 二、整体架构

```
┌─────────────────────────────────────────────────┐
│              React + ReactFlow 前端               │
│                                                   │
│  ┌──────────┐  ┌────────────────┐  ┌──────────┐  │
│  │ 节点流程图 │  │  Agent 详情面板 │  │ 指标图表  │  │
│  │(ReactFlow)│  │  (流式日志)     │  │(Recharts)│  │
│  └──────────┘  └────────────────┘  └──────────┘  │
└────────────────────────┬────────────────────────┘
                         │ WebSocket（实时推送）
                         │ REST API（历史查询）
┌────────────────────────▼────────────────────────┐
│              Go HTTP 服务（同进程）                │
│                                                   │
│  ┌──────────────┐   ┌──────────────────────────┐  │
│  │ WebSocket Hub │   │   REST API Handler        │  │
│  │  广播 Agent    │   │   /api/runs               │  │
│  │  状态事件     │   │   /api/agents             │  │
│  └──────────────┘   │   /api/metrics            │  │
│                     └──────────────────────────┘  │
└────────────────────────┬────────────────────────┘
                         │ Callback 注入（函数调用，零 IPC）
┌────────────────────────▼────────────────────────┐
│            Eino DeepAgent（主进程）               │
│                                                   │
│  ┌──────────────────────────────────────────┐     │
│  │         DashboardCallbackHandler          │     │
│  │  OnStart → 发送 agent_start 事件          │     │
│  │  OnEnd   → 发送 agent_end + 结果摘要      │     │
│  │  OnError → 发送 agent_error 事件          │     │
│  └──────────────────────────────────────────┘     │
│                                                   │
│  CodeComp  LitAgent  HypAgent  CodeAgent  ExpAgent │
│    🔵         🟡       🟢        ⚪         🔴      │  ← 节点颜色 = 状态
└─────────────────────────────────────────────────┘
```

**关键设计决策：Go 后端与 Eino Agent 同进程运行。**
- Callback → WebSocket Hub 是**函数调用**，无需序列化，无 IPC 开销
- 全 Go，无 Python 后端，不需要 FastAPI

---

## 三、Eino Callback → WebSocket 推送（核心链路）

### 3.1 事件结构定义

```go
// internal/dashboard/event.go

type EventType string

const (
    EventAgentStart   EventType = "agent_start"
    EventAgentEnd     EventType = "agent_end"
    EventAgentError   EventType = "agent_error"
    EventToolCall     EventType = "tool_call"
    EventToolResult   EventType = "tool_result"
    EventThinking     EventType = "thinking"       // LLM 流式输出
    EventStatusChange EventType = "status_change"
)

type AgentEvent struct {
    ID        string    `json:"id"`          // 事件唯一 ID (uuid)
    RunID     string    `json:"run_id"`      // 本次 Agent 运行 ID
    AgentName string    `json:"agent_name"`  // 触发事件的 Agent 名称
    Type      EventType `json:"type"`        // 事件类型
    Status    string    `json:"status"`      // idle / thinking / calling_tool / done / error
    Timestamp time.Time `json:"timestamp"`
    Payload   any       `json:"payload"`     // 附带数据（因事件类型而异）
}

// 附带数据示例
type AgentStartPayload struct {
    Input     string `json:"input"`       // 输入摘要（截断到 200 字）
    ParentID  string `json:"parent_id"`   // 父 Agent（用于绘制调用边）
}

type AgentEndPayload struct {
    Output     string        `json:"output"`      // 输出摘要
    DurationMs int64         `json:"duration_ms"` // 耗时
    TokenUsage *TokenUsage   `json:"token_usage"`
}

type ToolCallPayload struct {
    ToolName string `json:"tool_name"`
    Args     string `json:"args"`       // JSON 字符串（截断）
}

type TokenUsage struct {
    Input  int `json:"input"`
    Output int `json:"output"`
    Total  int `json:"total"`
}
```

### 3.2 WebSocket Hub

```go
// internal/dashboard/hub.go

type Hub struct {
    clients   map[*Client]bool
    broadcast chan []byte
    register  chan *Client
    leave     chan *Client
    mu        sync.RWMutex
}

var globalHub = &Hub{
    clients:   make(map[*Client]bool),
    broadcast: make(chan []byte, 256),
    register:  make(chan *Client),
    leave:     make(chan *Client),
}

func (h *Hub) Run() {
    for {
        select {
        case client := <-h.register:
            h.mu.Lock()
            h.clients[client] = true
            h.mu.Unlock()
        case client := <-h.leave:
            h.mu.Lock()
            delete(h.clients, client)
            close(client.send)
            h.mu.Unlock()
        case msg := <-h.broadcast:
            h.mu.RLock()
            for client := range h.clients {
                select {
                case client.send <- msg:
                default:
                    // 缓冲满，跳过（不阻塞 Agent 主流程）
                }
            }
            h.mu.RUnlock()
        }
    }
}

// Publish 供 Callback Handler 调用
func (h *Hub) Publish(event AgentEvent) {
    b, _ := json.Marshal(event)
    select {
    case h.broadcast <- b:
    default: // 非阻塞
    }
}
```

### 3.3 DashboardCallbackHandler（Eino 注入点）

```go
// internal/dashboard/callback.go

import (
    "github.com/cloudwego/eino/callbacks"
    ucb "github.com/cloudwego/eino/utils/callbacks"
    "github.com/cloudwego/eino/components/model"
    "github.com/cloudwego/eino/components/tool"
)

func NewDashboardHandler(hub *Hub, runID string) callbacks.Handler {
    startTimes := &sync.Map{} // agentName → time.Time

    return ucb.NewHandlerHelper().
        // ① 每个 LLM 调用开始
        ChatModel(&ucb.ModelCallbackHandler{
            OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *model.CallbackInput) context.Context {
                startTimes.Store(info.Name, time.Now())
                hub.Publish(AgentEvent{
                    ID:        uuid.New().String(),
                    RunID:     runID,
                    AgentName: info.Name,
                    Type:      EventAgentStart,
                    Status:    "thinking",
                    Timestamp: time.Now(),
                    Payload: AgentStartPayload{
                        Input: summarize(input.Messages),
                    },
                })
                return ctx
            },
            OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *model.CallbackOutput) context.Context {
                dur := int64(0)
                if t, ok := startTimes.Load(info.Name); ok {
                    dur = time.Since(t.(time.Time)).Milliseconds()
                }
                hub.Publish(AgentEvent{
                    ID:        uuid.New().String(),
                    RunID:     runID,
                    AgentName: info.Name,
                    Type:      EventAgentEnd,
                    Status:    "done",
                    Timestamp: time.Now(),
                    Payload: AgentEndPayload{
                        Output:     summarize([]*schema.Message{output.Message}),
                        DurationMs: dur,
                        TokenUsage: &TokenUsage{
                            Input:  output.TokenUsage.PromptTokens,
                            Output: output.TokenUsage.CompletionTokens,
                            Total:  output.TokenUsage.TotalTokens,
                        },
                    },
                })
                return ctx
            },
            OnError: func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
                hub.Publish(AgentEvent{
                    ID:        uuid.New().String(),
                    RunID:     runID,
                    AgentName: info.Name,
                    Type:      EventAgentError,
                    Status:    "error",
                    Timestamp: time.Now(),
                    Payload:   map[string]string{"error": err.Error()},
                })
                return ctx
            },
        }).
        // ② 工具调用
        Tool(&ucb.ToolCallbackHandler{
            OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *tool.CallbackInput) context.Context {
                hub.Publish(AgentEvent{
                    ID:        uuid.New().String(),
                    RunID:     runID,
                    AgentName: info.Name,
                    Type:      EventToolCall,
                    Status:    "calling_tool",
                    Timestamp: time.Now(),
                    Payload: ToolCallPayload{
                        ToolName: info.Name,
                        Args:     truncate(fmt.Sprintf("%v", input.Arguments), 300),
                    },
                })
                return ctx
            },
        }).
        Handler()
}
```

### 3.4 注入到 Eino Graph

```go
// cmd/agent/main.go

hub := dashboard.NewHub()
go hub.Run()

runID := uuid.New().String()
cbHandler := dashboard.NewDashboardHandler(hub, runID)

// 注入：影响整个 Graph 内所有节点
result, err := graph.Invoke(ctx, input,
    compose.WithCallbacks(cbHandler),
)
```

---

## 四、Go HTTP 服务（与 Agent 同进程）

```go
// internal/server/server.go

func StartServer(hub *dashboard.Hub, store *store.Store) {
    mux := http.NewServeMux()

    // WebSocket 端点
    mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
        dashboard.ServeWs(hub, w, r)
    })

    // REST：获取历史 Runs
    mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
        runs := store.ListRuns()
        json.NewEncoder(w).Encode(runs)
    })

    // REST：获取指定 Run 的所有事件（用于回放）
    mux.HandleFunc("/api/runs/", func(w http.ResponseWriter, r *http.Request) {
        runID := strings.TrimPrefix(r.URL.Path, "/api/runs/")
        events := store.GetEvents(runID)
        json.NewEncoder(w).Encode(events)
    })

    // REST：Agent 当前状态快照
    mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
        snapshot := store.GetAgentSnapshot()
        json.NewEncoder(w).Encode(snapshot)
    })

    // 静态文件（前端 build 产物）
    mux.Handle("/", http.FileServer(http.Dir("./dashboard/dist")))

    log.Println("Dashboard: http://localhost:8080")
    http.ListenAndServe(":8080", cors(mux))
}
```

---

## 五、事件持久化（Store）

```go
// internal/store/store.go

// 简单内存 Store，可替换为 SQLite
type Store struct {
    mu     sync.RWMutex
    runs   map[string]*RunRecord   // runID → Run
    events map[string][]AgentEvent // runID → events
    // 当前各 Agent 状态快照
    agentStatus map[string]string  // agentName → status
}

type RunRecord struct {
    ID        string    `json:"id"`
    StartTime time.Time `json:"start_time"`
    EndTime   *time.Time `json:"end_time,omitempty"`
    Status    string    `json:"status"` // running / done / error
}

func (s *Store) Append(event AgentEvent) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.events[event.RunID] = append(s.events[event.RunID], event)
    s.agentStatus[event.AgentName] = event.Status
}
```

---

## 六、前端 React 方案

### 6.1 技术选型

| 技术 | 用途 | 理由 |
|------|------|------|
| **React 18** | UI 框架 | 生态最成熟 |
| **ReactFlow v12** | Agent 节点图 | 专为有向图设计，对标 Multica 可视化 |
| **Zustand** | 状态管理 | 轻量，WebSocket 消息直接写入 store |
| **Recharts** | 指标图表 | Token 用量、耗时折线图 |
| **Tailwind CSS** | 样式 | 快速原型，暗色主题友好 |
| **Vite** | 构建工具 | 快 |

### 6.2 ReactFlow 节点定义

```tsx
// src/components/AgentNode.tsx

import { Handle, Position, NodeProps } from '@xyflow/react';

type AgentNodeData = {
  label: string;      // Agent 名称
  status: 'idle' | 'thinking' | 'calling_tool' | 'done' | 'error';
  lastOutput?: string;
  tokenUsage?: { input: number; output: number; total: number };
  durationMs?: number;
};

const STATUS_COLORS: Record<AgentNodeData['status'], string> = {
  idle:         'bg-gray-500',
  thinking:     'bg-yellow-400 animate-pulse',
  calling_tool: 'bg-blue-500 animate-pulse',
  done:         'bg-green-500',
  error:        'bg-red-500',
};

export function AgentNode({ data }: NodeProps<AgentNodeData>) {
  return (
    <div className="rounded-xl border border-gray-700 bg-gray-900 p-4 min-w-[180px] shadow-lg">
      <Handle type="target" position={Position.Left} />
      
      {/* 状态指示器 + 名称 */}
      <div className="flex items-center gap-2">
        <span className={`w-3 h-3 rounded-full ${STATUS_COLORS[data.status]}`} />
        <span className="text-white font-semibold">{data.label}</span>
      </div>
      
      {/* 状态文字 */}
      <div className="mt-1 text-xs text-gray-400">{data.status}</div>
      
      {/* Token 用量 */}
      {data.tokenUsage && (
        <div className="mt-2 text-xs text-gray-500">
          🪙 {data.tokenUsage.total} tokens | ⏱ {data.durationMs}ms
        </div>
      )}
      
      {/* 最近输出摘要 */}
      {data.lastOutput && (
        <div className="mt-2 text-xs text-gray-400 truncate max-w-[160px]">
          {data.lastOutput}
        </div>
      )}
      
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
```

### 6.3 Zustand Store（WebSocket 驱动）

```tsx
// src/store/agentStore.ts

import { create } from 'zustand';
import type { Node, Edge } from '@xyflow/react';

interface AgentState {
  nodes: Node[];
  edges: Edge[];
  events: AgentEvent[];
  ws: WebSocket | null;
  connect: (url: string) => void;
  handleEvent: (event: AgentEvent) => void;
}

export const useAgentStore = create<AgentState>((set, get) => ({
  nodes: INITIAL_NODES,   // 预定义的6个 Agent 节点位置
  edges: INITIAL_EDGES,   // 预定义的调用关系边
  events: [],
  ws: null,

  connect(url) {
    const ws = new WebSocket(url);
    ws.onmessage = (e) => {
      const event: AgentEvent = JSON.parse(e.data);
      get().handleEvent(event);
    };
    set({ ws });
  },

  handleEvent(event) {
    set((state) => ({
      // 更新事件列表
      events: [...state.events, event],
      
      // 更新对应节点状态
      nodes: state.nodes.map((node) =>
        node.id === event.agent_name
          ? {
              ...node,
              data: {
                ...node.data,
                status: event.status,
                lastOutput:
                  event.type === 'agent_end'
                    ? (event.payload as any).output
                    : node.data.lastOutput,
                tokenUsage:
                  event.type === 'agent_end'
                    ? (event.payload as any).token_usage
                    : node.data.tokenUsage,
                durationMs:
                  event.type === 'agent_end'
                    ? (event.payload as any).duration_ms
                    : node.data.durationMs,
              },
            }
          : node
      ),
    }));
  },
}));
```

### 6.4 主页面布局

```tsx
// src/App.tsx

import { ReactFlow, Background, Controls, MiniMap } from '@xyflow/react';
import { useAgentStore } from './store/agentStore';
import { AgentNode } from './components/AgentNode';
import { EventLog } from './components/EventLog';
import { MetricsPanel } from './components/MetricsPanel';

const nodeTypes = { agentNode: AgentNode };

export default function App() {
  const { nodes, edges, events, connect } = useAgentStore();

  useEffect(() => {
    connect('ws://localhost:8080/ws');
  }, []);

  return (
    <div className="flex h-screen bg-gray-950 text-white">
      {/* 主区域：Agent 节点图 */}
      <div className="flex-1">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          fitView
        >
          <Background color="#374151" />
          <Controls />
          <MiniMap nodeColor={(n) => STATUS_COLORS[n.data.status as any]} />
        </ReactFlow>
      </div>

      {/* 右侧面板 */}
      <div className="w-80 flex flex-col border-l border-gray-800">
        {/* 指标图表 */}
        <MetricsPanel events={events} />
        
        {/* 实时事件日志 */}
        <EventLog events={events} />
      </div>
    </div>
  );
}
```

### 6.5 初始节点布局（6 Agent 预设）

```tsx
// src/constants/initialGraph.ts

export const INITIAL_NODES = [
  { id: 'DeepAgent',    type: 'agentNode', position: { x: 400, y: 200 }, data: { label: '🧠 DeepAgent (总调度)', status: 'idle' } },
  { id: 'CodeComp',     type: 'agentNode', position: { x: 100, y: 50  }, data: { label: '📁 CodeCompAgent', status: 'idle' } },
  { id: 'LitAgent',     type: 'agentNode', position: { x: 100, y: 150 }, data: { label: '📚 LitAgent', status: 'idle' } },
  { id: 'HypAgent',     type: 'agentNode', position: { x: 100, y: 250 }, data: { label: '💡 HypAgent', status: 'idle' } },
  { id: 'CodeAgent',    type: 'agentNode', position: { x: 100, y: 350 }, data: { label: '⚙️ CodeAgent', status: 'idle' } },
  { id: 'ExpAgent',     type: 'agentNode', position: { x: 100, y: 450 }, data: { label: '🔬 ExpAgent', status: 'idle' } },
  { id: 'PaperAgent',   type: 'agentNode', position: { x: 700, y: 200 }, data: { label: '📝 PaperAgent', status: 'idle' } },
];

export const INITIAL_EDGES = [
  { id: 'e1', source: 'DeepAgent', target: 'CodeComp',   animated: false, style: { stroke: '#6b7280' } },
  { id: 'e2', source: 'DeepAgent', target: 'LitAgent',   animated: false, style: { stroke: '#6b7280' } },
  { id: 'e3', source: 'LitAgent',  target: 'HypAgent',   animated: false, style: { stroke: '#6b7280' } },
  { id: 'e4', source: 'HypAgent',  target: 'CodeAgent',  animated: false, style: { stroke: '#6b7280' } },
  { id: 'e5', source: 'CodeAgent', target: 'ExpAgent',   animated: false, style: { stroke: '#6b7280' } },
  { id: 'e6', source: 'ExpAgent',  target: 'PaperAgent', animated: false, style: { stroke: '#6b7280' } },
];

// 当某条边"激活"时，动态设置 animated: true + 颜色高亮
```

---

## 七、项目目录结构

```
mingkunsearch/
├── cmd/
│   └── agent/
│       └── main.go              # 入口：启动 Agent + HTTP Server
│
├── internal/
│   ├── dashboard/
│   │   ├── callback.go          # DashboardCallbackHandler（Eino 注入）
│   │   ├── event.go             # 事件结构定义
│   │   ├── hub.go               # WebSocket Hub
│   │   └── ws.go                # ServeWs 处理函数
│   ├── server/
│   │   └── server.go            # HTTP 路由
│   └── store/
│       └── store.go             # 事件/状态持久化（内存 or SQLite）
│
├── agents/                      # 各 Agent 实现（Eino）
│   ├── deep_agent.go
│   ├── lit_agent.go
│   ├── hyp_agent.go
│   ├── code_agent.go
│   ├── exp_agent.go
│   └── paper_agent.go
│
├── dashboard/                   # 前端（React + ReactFlow）
│   ├── src/
│   │   ├── App.tsx
│   │   ├── store/
│   │   │   └── agentStore.ts    # Zustand store（WebSocket 驱动）
│   │   ├── components/
│   │   │   ├── AgentNode.tsx    # ReactFlow 自定义节点
│   │   │   ├── EventLog.tsx     # 实时事件日志
│   │   │   └── MetricsPanel.tsx # Token/耗时图表
│   │   └── constants/
│   │       └── initialGraph.ts  # 初始节点/边定义
│   ├── package.json
│   └── vite.config.ts
│
└── go.mod
```

---

## 八、开发顺序（推荐）

### Phase 1：最小可用版本（1-2天）

1. 搭 WebSocket Hub + 简单 REST server（约 100 行 Go）
2. 实现 `DashboardCallbackHandler`，注入已有 Eino Graph
3. 前端：只做节点图 + WebSocket 连接，节点颜色随状态变化
4. 验证：跑一个 Agent，看到节点从灰→黄→绿

### Phase 2：日志 + 指标（2-3天）

1. 右侧面板：实时事件日志（按时间倒序）
2. Recharts 折线图：Token 用量累计、各 Agent 耗时柱状图
3. 点击节点展开详情（最近 5 条 LLM 消息摘要）

### Phase 3：历史回放（可选，3-4天）

1. Store 改用 SQLite 持久化
2. `/api/runs` 返回历史 Run 列表
3. 前端加时间轴，可选择历史 Run 回放

---

## 九、关键依赖

### Go 端

```go
// go.mod
require (
    github.com/cloudwego/eino          v0.8.x
    github.com/cloudwego/eino-ext      v0.8.x
    github.com/gorilla/websocket       v1.5.3
    github.com/google/uuid             v1.6.x
    // 可选：SQLite 持久化
    github.com/mattn/go-sqlite3        v1.14.x
)
```

### 前端

```json
{
  "dependencies": {
    "@xyflow/react": "^12.x",
    "recharts":      "^2.x",
    "zustand":       "^5.x",
    "tailwindcss":   "^3.x",
    "react":         "^18.x"
  }
}
```

---

## 十、核心优势（与 Multica 对比）

| 维度 | Multica | 我们的方案 |
|------|---------|-----------|
| 面向场景 | 通用代码 Agent 管理 | **科研 Agent 专用**（Agent 间有明确流水线） |
| 节点图 | 无（列表视图） | **ReactFlow 有向图**，清晰展示调用拓扑 |
| 后端 | Go（多进程架构） | **Go 同进程**，Callback 直连 WebSocket Hub |
| 实时粒度 | Task 级别 | **每次 LLM 调用 + 每次 Tool 调用** |
| 流式输出 | 不支持 | **LLM 流式 Token 逐字显示**（OnEndWithStreamOutput） |
| 部署 | Docker Compose（多容器） | **单二进制** + 静态前端文件 |

---

*文档版本：v1.0 | 最后更新：2026-04-18 | 对标：Multica 可视化 + Eino Callback*
