# MingkunSearch — 代码理解 Agent 设计方案

> 目标：让 Agent 能够理解用户提供的本地代码库，梳理代码目的、架构、研究进展，并在此基础上继续研究。
> 
> 工作目录：`D:\mingkunsearch`  
> 文档版本：v1.0 | 最后更新：2026-04-17

---

## 一、核心需求分析

### 1.1 用户场景

```
用户有一份现有代码（本地文件夹）：
  ├── models/
  │   ├── gcn.py          # GCN 模型实现
  │   ├── graphsage.py    # GraphSAGE 实现
  │   └──gat.py           # GAT 实现（刚写了框架，未跑通）
  ├── data/
  │   └── cora.py         # 数据加载
  ├── train.py            # 训练入口
  ├── evaluate.py         # 评估脚本
  ├── results/            # 已有实验结果
  │   ├── gcn_81.5.json
  │   └── graphsage_84.2.json
  └── README.md           # 可能过时的说明
```

**用户期望：**
1. Agent 自动扫描代码，理解项目在做什么
2. 梳理研究进展（已验证了什么、卡在哪里）
3. 分析代码质量（GAT 没跑通的原因）
4. 基于已有成果继续研究（完善 GAT、尝试新方法）

### 1.2 与"从零开始"的关键差异

| 维度 | 从零开始 | 继续研究 |
|------|---------|---------|
| **代码理解** | Agent 生成的代码，天然理解 | 需要逆向理解用户代码 |
| **研究状态** | 空白，从假设开始 | 需要从结果/日志/代码推断进度 |
| **可复用性** | Agent 自己生成，可控 | 需要判断哪些代码可复用 |
| **知识注入** | 全靠记忆系统 | 代码库本身就是最大的"冷记忆" |

---

## 二、方案选型：基于 Tree-Sitter + 代码知识图谱

### 2.1 调研结论

调研了 3 个前沿代码理解方案：

| 方案 | 核心技术 | 优势 | 劣势 | 我们的借鉴 |
|------|---------|------|------|----------|
| **Codebase-Memory** (2026) | Tree-Sitter AST + 知识图谱 + SQLite | 66 语言、零依赖、6 秒索引 49K 节点 | C 语言实现，需要 CGO 或独立进程调用 | **核心借鉴**：索引架构 + 图 Schema |
| **LocAgent** (耶鲁 2025) | 异构代码图 + BFS 遍历 + LLM 图推理 | SWE-Bench 77.37% 函数级定位 | 偏代码定位，非理解 | 借鉴：图遍历 Tool 设计 |
| **Code Researcher** (Microsoft 2025) | 深度研究 + 结构化记忆 + Git 历史 | Linux 内核级复杂代码理解 | 偏 Bug 修复，非科研 | 借鉴：Git 历史分析 |

### 2.2 技术选型

```
代码解析：   Tree-Sitter（Go binding：go-tree-sitter）
代码索引：   SQLite（零依赖，和 Codebase-Memory 同方案）
图遍历：     自研 Go 图库（ adjacency list + BFS/DFS ）
嵌入向量：   不需要（基于图结构查询，不需要语义 Embedding）
增量同步：   XXH3 哈希 + fsnotify 文件监控
```

**为什么不用 RAG？** 代码理解的核心是结构关系（谁调用谁、数据怎么流），纯向量检索无法回答"train.py 里调用了哪个模型"、"evaluate.py 用了哪些指标"。知识图谱才是正解。

---

## 三、代码知识图谱 Schema

### 3.1 节点类型

| 节点 | 来源 | 属性 |
|------|------|------|
| **Project** | 根目录 | name, path, language |
| **File** | 文件系统 | path, language, loc |
| **Module** | Python/Go 包 | name, file_id |
| **Class** | Tree-Sitter AST | name, file_id, line_start, line_end |
| **Function** | Tree-Sitter AST | name, file_id, line_start, line_end, signature, docstring |
| **Variable** | 局部分析 | name, type, scope |
| **ExperimentResult** | results/ 目录 | name, metrics, timestamp, status |

### 3.2 边类型

| 边 | 语义 | 示例 |
|----|------|------|
| `CONTAINS` | 目录/文件包含 | Project → File → Class → Function |
| `CALLS` | 函数调用 | `train()` → `model.forward()` |
| `IMPORTS` | 模块导入 | train.py → `from models.gcn import GCN` |
| `INHERITS` | 类继承 | `GraphSAGE` → `GCN` |
| `DEFINES` | 定义关系 | File → Function |
| `USES_DATASET` | 数据集使用 | `train()` → Cora |
| `PRODUCES_RESULT` | 产出结果 | `train()` → gcn_81.5.json |
| `COMPUTES_METRIC` | 指标计算 | `evaluate()` → accuracy |

### 3.3 科研专用扩展

标准代码图谱之上，我们增加科研领域特有的节点和边：

```
┌─────────────────────────────────────────────────────┐
│  科研代码知识图谱（扩展）                              │
│                                                      │
│  标准节点：File, Class, Function, Variable           │
│                                                      │
│  科研节点（新增）：                                    │
│  ├── Model         # 神经网络模型（GCN, GAT...）       │
│  ├── Dataset       # 数据集（Cora, Citeseer...）      │
│  ├── LossFn        # 损失函数                        │
│  ├── Optimizer     # 优化器                          │
│  ├── Metric        # 评估指标（Accuracy, F1...）      │
│  ├── HyperParam    # 超参数配置                      │
│  └── Result        # 实验结果                        │
│                                                      │
│  科研边（新增）：                                     │
│  ├── Model IMPLIES Class     # "GraphSAGE 是一个类"  │
│  ├── Function USES Model     # "train() 用了 GCN"   │
│  ├── Function USES Dataset   # "train() 用了 Cora"  │
│  ├── Function SETS HyperParam                     │
│  ├── Function COMPUTES Metric                     │
│  └── Function PRODUCES Result                      │
└─────────────────────────────────────────────────────┘
```

---

## 四、CodeComprehension Agent 工作流

### 4.1 整体流程

```
用户提供代码路径: D:\research\gnn-project
         │
         ▼
┌──────────────────────────────────────────────────────┐
│  Step 1: 代码索引（CodebaseMemoryIndexer）              │
│  ├── 扫描目录结构                                      │
│  ├── Tree-Sitter AST 解析每个源文件                     │
│  ├── 提取函数/类/导入/调用关系                          │
│  ├── 科研实体识别（模型/数据集/指标/超参）               │
│  ├── 构建 SQLite 知识图谱                              │
│  └── 扫描 results/ 目录，关联实验结果                   │
└──────────────────────┬───────────────────────────────┘
                       │ 图谱构建完成
                       ▼
┌──────────────────────────────────────────────────────┐
│  Step 2: 代码理解（CodeComprehensionAgent）             │
│  ├── 架构摘要：主要模块、调用链、数据流                   │
│  ├── 研究进展梳理：已验证方法、结果对比                   │
│  ├── Gap 分析：未完成部分、潜在问题                      │
│  ├── 代码质量评估：可复用性、需要修改的部分               │
│  └── 输出结构化报告 → 写入 HOT Memory                   │
└──────────────────────┬───────────────────────────────┘
                       │ 理解完成
                       ▼
┌──────────────────────────────────────────────────────┐
│  Step 3: 继续研究（转入正常 Agent 工作流）               │
│  ├── HypothesisAgent 基于已有进展生成新假设              │
│  ├── CodeAgent 在已有代码基础上修改（非从零生成）         │
│  ├── ExpAgent 复用已有训练/评估脚本                      │
│  └── PaperAgent 基于已有结果扩展论文                     │
└──────────────────────────────────────────────────────┘
```

### 4.2 Step 1 详解：代码索引

```go
// internal/codeindex/indexer.go

type Indexer struct {
    db        *sql.DB         // SQLite 知识图谱
    parser    *treeSitterPool // Tree-Sitter 解析池
    project   string          // 项目根路径
}

// 索引流程
func (idx *Indexer) Index(ctx context.Context, projectPath string) error {
    // 1. 扫描文件系统
    files := idx.scanFiles(projectPath)
    
    // 2. 并行 AST 解析（goroutine 池）
    var wg sync.WaitGroup
    for _, file := range files {
        wg.Add(1)
        go func(f FileInfo) {
            defer wg.Done()
            ast := idx.parser.Parse(f.Path, f.Language)
            entities := idx.extractEntities(ast)    // 函数/类/导入
            calls := idx.extractCalls(ast)           // 调用关系
            idx.storeToGraph(f, entities, calls)
        }(file)
    }
    wg.Wait()
    
    // 3. 科研实体识别
    idx.identifyResearchEntities()
    
    // 4. 扫描实验结果
    idx.indexExperimentResults(filepath.Join(projectPath, "results/"))
    
    // 5. Louvain 社区检测（可选，架构分析用）
    idx.detectCommunities()
    
    return nil
}
```

### 4.3 Step 2 详解：代码理解

Agent 通过图遍历 Tool 来理解代码，而非把所有代码塞进上下文：

```go
// 给 CodeComprehensionAgent 的 Tool 集合

tools := []tool.BaseTool{
    // 图查询工具
    &SearchSymbolTool{},        // "搜索名为 GCN 的类"
    &TraceCallChainTool{},      // "从 train() 追踪调用链"
    &GetCodeSnippetTool{},      // "获取 gcn.py 第 10-50 行"
    &GetArchitectureTool{},     // "获取项目架构摘要"
    
    // 科研专用工具
    &ListModelsTool{},          // "列出所有已实现的模型"
    &ListResultsTool{},         // "列出所有实验结果"
    &CompareResultsTool{},      // "对比 GCN 和 GraphSAGE 的结果"
    &FindUnfinishedCode{},      // "找出未完成的代码（TODO、空函数）"
    
    // Git 工具
    &GitHistoryTool{},          // "查看最近 10 次提交"
    &GitDiffTool{},             // "查看某次提交改了什么"
}
```

**LLM 推理过程（多步工具调用）：**

```
用户：帮我理解这个 GNN 项目

Agent: [调用 GetArchitectureTool]
→ 返回: 项目有 3 个模型(GCN/GraphSAGE/GAT)、1 个训练脚本、1 个评估脚本

Agent: [调用 ListResultsTool]  
→ 返回: GCN=81.5%, GraphSAGE=84.2%, GAT=无结果

Agent: [调用 SearchSymbolTool, query="GAT"]
→ 返回: gat.py 第 15 行定义了 GAT 类

Agent: [调用 GetCodeSnippetTool, file="gat.py", range="15-60"]
→ 返回: GAT 类代码，发现 forward() 方法未实现注意力权重

Agent: [调用 TraceCallChainTool, from="train.py"]
→ 返回: train.py → load_data() → train_epoch() → model.forward()

Agent: [调用 FindUnfinishedCode]
→ 返回: gat.py: forward() 只有 pass, evaluate.py 缺少 GAT 评估分支

Agent 输出:
"这是一个图神经网络节点分类项目，基于 PyTorch Geometric。
- 已实现 3 个模型：GCN、GraphSAGE、GAT
- GCN baseline 在 Cora 上达到 81.5% 准确率
- GraphSAGE 两层聚合提升到 84.2%
- GAT 框架已搭好但 forward() 未实现（第 30 行只有 pass）
- evaluate.py 没有针对 GAT 的评估分支
- 建议：完成 GAT 的多头注意力实现，预计可提升到 85%+"
```

### 4.4 Step 3 详解：继续研究

理解完成后，生成结构化报告写入 HOT Memory，后续 Agent 在此基础上工作：

```markdown
# CURRENT_STATE.md（代码理解后更新）

## 项目概况
- 方向：GNN 节点分类
- 框架：PyTorch Geometric
- 数据集：Cora

## 已实现模型
| 模型 | 状态 | 准确率 | 文件 |
|------|------|--------|------|
| GCN (2层) | ✅ 已验证 | 81.5% | models/gcn.py |
| GraphSAGE (2层) | ✅ 已验证 | 84.2% | models/graphsage.py |
| GAT (未完成) | ❌ forward()未实现 | — | models/gat.py |

## 研究进展
- [x] 数据加载和预处理（data/cora.py）
- [x] GCN baseline
- [x] GraphSAGE 改进（+2.7%）
- [ ] GAT 注意力机制（TODO: 第30行）
- [ ] 更好的聚合器（GIN?）
- [ ] 多数据集验证（Citeseer, Pubmed）

## 下一步建议
1. 完成 GAT 的多头注意力 forward()
2. 在 evaluate.py 添加 GAT 分支
3. 对比三种方法，撰写实验表格
```

---

## 五、索引更新与版本管理

### 5.1 增量更新

```go
// 文件变更监控
type Watcher struct {
    hasher *xxhash.XXHash64
    db     *sql.DB
}

func (w *Watcher) Watch(ctx context.Context, projectPath string) {
    // 1. 计算每个文件的 XXH3 哈希
    // 2. 与数据库中存储的哈希对比
    // 3. 有变化的文件：重新 AST 解析 + 更新图谱
    // 4. 删除的文件：移除对应节点和边
    // 5. 新增的文件：完整解析入库
    
    // 使用 fsnotify 监控文件系统事件（可选）
}
```

### 5.2 代码快照

每次研究迭代前保存代码快照：

```go
type CodeSnapshot struct {
    ID        string    // 快照 ID
    Timestamp time.Time
    Commit    string    // Git commit hash（如果有）
    Description string  // "完成 GraphSAGE 实现"
    GraphDB   string    // SQLite 知识图谱副本路径
}
```

这样 BFTS 树搜索的每个节点不仅保存实验代码，还保存对应的代码知识图谱状态。

---

## 六、与现有架构的集成

### 6.1 新增 Agent：CodeComprehensionAgent

| Agent | 职责 | 触发时机 |
|-------|------|---------|
| **CodeComprehensionAgent** | 理解已有代码库，生成项目报告 | 用户首次提供代码路径时 |

### 6.2 更新后的工作流

```
用户输入: 代码路径 + 研究任务（可选）
     │
     ▼
┌────────────────────┐     首次     ┌──────────────────────────┐
│   CodeComprehension │───────────▶│ 代码索引 + 知识图谱构建   │
│      Agent          │             └────────────┬─────────────┘
└────────┬───────────┘                          │
         │ 项目报告                              ▼
         ▼                              HOT Memory 更新
┌────────────────────┐
│  正常 Agent 工作流  │  ← CodeAgent/HypothesisAgent 等可查询知识图谱
│  Lit→Hyp→Code→Exp  │
└────────────────────┘
```

### 6.3 CodeAgent 的增强

原来的 CodeAgent 从零生成代码，现在增强为：

```go
// CodeAgent 增加的 Tool
codeAgentTools := []tool.BaseTool{
    // 原有
    shellTool, gitTool, dockerTool,
    
    // 新增：基于代码图谱的精确修改
    &GetCodeSnippetTool{},     // "获取 gat.py 第 30 行代码"
    &TraceCallChainTool{},     // "查看 forward() 被谁调用"
    &ModifyCodeTool{},         // "修改 gat.py 第 30 行，插入注意力代码"
    &AddImportTool{},          // "在 gat.py 添加 import torch.nn.functional as F"
}
```

关键变化：**CodeAgent 不再从零写代码，而是基于图谱精确定位 → 修改指定位置 → 验证修改不影响其他模块。**

---

## 七、更新后的项目目录结构

```
D:\mingkunsearch\
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── agents/
│   │   ├── literature.go
│   │   ├── hypothesis.go
│   │   ├── code.go                    # 增强：支持图谱查询
│   │   ├── experiment.go
│   │   ├── paper.go
│   │   ├── deepagent.go
│   │   └── code_comprehension.go      # 🆕 代码理解 Agent
│   ├── codeindex/                     # 🆕 代码索引模块
│   │   ├── indexer.go                 # Tree-Sitter 索引器
│   │   ├── parser.go                  # AST 解析 + 实体提取
│   │   ├── graph.go                   # 图数据结构 + 遍历
│   │   ├── schema.go                  # 图 Schema 定义
│   │   ├── research.go                # 科研实体识别器
│   │   ├── snapshot.go                # 代码快照管理
│   │   └── watcher.go                 # 增量更新监控
│   ├── tools/
│   │   ├── arxiv.go
│   │   ├── semantic_scholar.go
│   │   ├── docker.go
│   │   ├── mlflow.go
│   │   ├── shell.go
│   │   ├── file.go
│   │   ├── graph_search.go            # 🆕 图查询 Tool
│   │   ├── graph_trace.go             # 🆕 调用链追踪 Tool
│   │   ├── graph_code.go              # 🆕 代码片段获取 Tool
│   │   ├── graph_arch.go              # 🆕 架构摘要 Tool
│   │   ├── research_list.go           # 🆕 列出模型/结果 Tool
│   │   └── research_compare.go        # 🆕 结果对比 Tool
│   ├── memory/
│   ├── bfts/
│   ├── dashboard/
│   └── graph/
├── web/
├── sandbox/
├── memory/
├── experiments/
├── data/
├── go.mod
└── RESEARCH_AGENT_DESIGN.md
```

---

## 八、开发阶段更新

### 新增 Phase 0.5：代码理解模块（第 1-2 周穿插）

在原有 Phase 0 基础设施中增加代码理解能力：

- [ ] 集成 go-tree-sitter，支持 Python + Go 语言解析
- [ ] 实现代码索引器（AST → SQLite 图谱）
- [ ] 实现图遍历 Tool（SearchSymbol / TraceCallChain / GetCodeSnippet）
- [ ] 实现科研实体识别（模型/数据集/指标自动检测）
- [ ] 实现实验结果扫描（解析 results/ 目录）
- [ ] 实现 CodeComprehensionAgent（多步工具调用 → 生成项目报告）
- [ ] 测试：给定一个真实 GNN 项目，输出正确的项目理解报告

**验收标准：** 输入 `D:\research\gnn-project`，Agent 能输出：
1. 项目在做什么（一句话）
2. 已实现哪些方法、结果如何
3. 哪些代码未完成
4. 下一步建议

---

## 九、关键技术细节

### 9.1 Tree-Sitter Go 集成

```go
// Tree-Sitter 解析 Python 文件
import "github.com/smacker/go-tree-sitter"

parser := tree_sitter.NewParser()
parser.SetLanguage(tree_sitter_python.Language())

sourceCode, _ := os.ReadFile("models/gcn.py")
tree := parser.Parse(nil, sourceCode)

// 遍历 AST 提取函数定义
rootNode := tree.RootNode()
query, _ := tree_sitter.NewQuery(
    tree_sitter_python.Language(),
    "(function_definition name: (identifier) @name body: (block) @body) @func",
)
cursor := query.Exec(rootNode)
for cursor.NextMatch() {
    funcName := sourceCode[cursor.Capture(0).Node.StartByte():cursor.Capture(0).Node.EndByte()]
    // 存入图谱
}
```

### 9.2 图遍历性能

```
项目规模          索引时间      查询延迟      存储大小
─────────────────────────────────────────────────────
小型（<50文件）  < 1 秒       < 1ms        < 1MB
中型（200文件）  < 3 秒       < 1ms        < 5MB
大型（1000文件） < 10 秒      < 5ms        < 50MB
```

### 9.3 科研实体识别策略

```go
// 启发式 + LLM 混合策略
func identifyResearchEntities(ast *tree_sitter.Tree, source []byte) []ResearchEntity {
    var entities []ResearchEntity
    
    // 1. 启发式规则（零成本，快速匹配）
    // 检测类继承关系：class GCN(torch.nn.Module) → 模型
    // 检测函数调用：F.cross_entropy → 损失函数
    // 检测导入：from torch_geometric.datasets import Cora → 数据集
    // 检测参数：lr=0.01, hidden_dim=64 → 超参数
    
    // 2. LLM 确认（可选，仅对不确定的实体）
    // 对于启发式无法判断的，用 LLM 确认一次
    // （整个项目只需调用 1-2 次 LLM）
    
    return entities
}
```

---

*文档版本：v1.0 | 最后更新：2026-04-17*
