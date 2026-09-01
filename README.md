<div align="center">

# Multica Gateway Fork

**把 Multica 当作多 Agent 聚合网关，而不是工作流框架。**

[个人维护分支](https://github.com/mameikagou/multica/tree/local/native-context-chat-handoff) ·
[上游项目](https://github.com/multica-ai/multica) ·
[上游文档](https://multica.ai/docs)

</div>

> [!IMPORTANT]
> 这是 `multica-ai/multica` 的个人发行分支，不是官方版本。它保留 Multica 优秀的传输、路由、运行时注册、聊天界面、执行日志和用量统计能力，同时尽量不接管 Codex、Claude Code、Cursor、Pi 等 Agent/Harness 原本的工作方式。

## 这条维护分支的价值

这不是一个“比上游多几个开关”的补丁堆，而是一个长期维护的 **Agent 网关发行版**：它保留 Multica 作为多端入口和运行时调度器的价值，同时把工作目录、会话、Prompt、Skill 和代码目录的最终控制权交还给用户和 Agent。

- **官方客户端，自维护网关。** Web、Desktop 和 Mobile 可以继续跟随官方版本；本机只需替换同一个 Go CLI/daemon，不需要 fork 整个客户端才能获得核心体验。
- **真实 cwd，不污染代码目录。** 所有 Agent/Harness 可从用户指定的 `native_workdir` 启动，Multica 的日志、凭证、provider home 和 task scratch 保持在独立的私有状态目录。
- **连续会话，不用平台工作流换取历史。** Codex thread 和 Pi/OMP session 不再因 workdir 变化、取消 turn 或追加消息轻易冷启动；Web 私聊恢复后只发送新增消息，不在每个 turn 重放稳定的平台套话。
- **平台能力可选，不强制接管。** `minimal` 保留必要的环境事实与 Skill 可见性，`off` 移除 Multica 运行时工作流，`full` 则保留官方行为。
- **把用户代码与可回收 workspace 分开。** daemon 的 GC 只能修改能证明归属于 Multica task 的受管路径；`native_workdir` 不会被当成可丢弃的 task workspace。
- **通用修复回馈上游，个人取舍留在 fork。** 可复用的协议、会话和 CLI 修复会拆成独立 PR 提交给 Multica；“网关而非工作流”这种明确带有个人偏好的组合仍由本分支维护。

## 已被上游合并的贡献

截至 2026-09-01，[`mameikagou`](https://github.com/mameikagou) 向 [`multica-ai/multica`](https://github.com/multica-ai/multica) 提交的以下 5 个 PR 已全部正式合并（日期按 GitHub UTC）：

| PR | 上游获得的能力 | 合并日期 |
| --- | --- | --- |
| [#7331 · daemon server URL override](https://github.com/multica-ai/multica/pull/7331) | 自部署环境可用 `MULTICA_DAEMON_SERVER_URL` 分离公网 webhook 地址与 daemon 私有出口 | 2026-08-24 |
| [#7557 · remove obsolete autopilot priority flag](https://github.com/multica-ai/multica/pull/7557) | 移除已废弃但仍被 CLI 静默接受的 autopilot priority 参数，让 CLI、API 与产品语义一致 | 2026-08-26 |
| [#7756 · explicit Codex Standard speed](https://github.com/multica-ai/multica/pull/7756) | 区分“继承本地配置”、“明确 Standard”与“Fast”，支持本地默认 Fast 时主动切回 Standard | 2026-09-01 |
| [#7760 · Pi session continuity](https://github.com/multica-ai/multica/pull/7760) | Pi/OMP 使用独立 JSONL session 时不再被 workdir 变化错误阻断恢复 | 2026-08-31 |
| [#7790 · Codex thread handshake budget](https://github.com/multica-ai/multica/pull/7790) | 为 `thread/start` / `thread/resume` 设置独立的 60 秒默认预算，轻量 RPC 继续保持 30 秒 | 2026-08-31 |

PR 已合并意味着这些通用修复不再是 fork 独占卖点，而是所有上游用户都能受益。上表是作者的上游贡献记录，不等于当前分支逐个 cherry-pick 了每个 merge commit；维护分支只显式整合与网关模式直接相关的改动，其余在下次 mainline sync 时吸收。

本分支的持续价值在于：将通用上游能力与 native cwd、低侵入上下文、严格的目录归属边界和追加消息交接组合成一套可直接日常使用的网关模式。

## 这个分支解决什么问题

Multica 原本不仅是网关，也会主动塑造 Agent 的执行环境：为每次任务创建独立 workdir，向项目写入运行说明和 Skill，要求模型遵循 Multica 的 issue/comment 工作流，并把 provider session 与任务目录绑定。

对需要完整项目管理闭环的团队，这套设计有价值；但对于已经有成熟本地工作流、只希望统一接入多个 Agent 的用户，它会产生几个问题：

- Agent 不在真正的代码根目录运行，还需要进入 Multica 生成的任务目录；
- `AGENTS.md`、`CLAUDE.md`、`.agent_context/`、项目 MCP 配置等 sidecar 可能污染用户目录；
- 强制工作流和 Skill 激活规则让模型过度敏感，容易调用与当前任务无关的 Skill；
- 取消正在运行的 turn 后，替代任务可能在旧目录释放前冷启动，丢失 provider session；
- 网关自己的提示词和工作流消耗大量上下文，掩盖了 provider 原生体验。

这个分支的原则很简单：

```text
Multica 负责：连接、路由、认证、消息、日志、用量、附件和运行状态
Agent 负责：  cwd、会话、项目规则、Skill 选择、工具使用和代码修改
用户负责：   决定任务如何拆分，以及多个 Agent 是否同时操作同一目录
```

## 网关模式架构

```text
Web / Desktop / Mobile
          │
          │  chat、task、附件、日志、usage
          ▼
    Multica control plane
          │
          │  WebSocket task dispatch
          ▼
   本分支 CLI / daemon
          │
          ├── 私有状态：~/.local/share/multica/workspaces/...
          │              logs、credentials、provider home、task scratch
          │
          └── 统一 cwd：<native_workdir>
                        Codex / Claude / Cursor / Pi / 其他 provider
```

`native_workdir` 是用户拥有的真实目录。daemon 可以让所有 provider 从这里启动，但不会把 Multica sidecar 写进去。显式绑定到项目的 `local_directory` 仍然优先，因为那代表用户针对该任务主动选择了另一个目录。

## 维护分支提供和整合了什么

| 问题或边界 | 本分支行为 | 价值 |
| --- | --- | --- |
| 每次任务默认使用 daemon 管理的 workdir | 新增通用 `native_workdir`，所有 Agent/Harness 都可直接在真实代码根目录运行 | 保留 provider 原生项目发现和用户工作流 |
| 向 cwd 写入 `AGENTS.md`、`CLAUDE.md`、`.agent_context/` 等上下文 | native 模式不写任何 Multica runtime sidecar | 用户目录保持原样 |
| Reasonix 写 `reasonix.toml`，Cursor 写 `.cursor/mcp.json` | native 模式绕过这些项目级写入 | 不覆盖用户自己的 provider 配置 |
| 默认注入完整 Multica workflow 和 Skill 激活策略 | 新增 `full`、`minimal`、`off` 三档 `platform_context_mode` | 把工作流变成可选能力 |
| 服务端内置 Skill 总是随任务进入 provider 环境 | `minimal` 只做中性展示；`off` 会过滤 Multica 内置 Skill，用户安装和 workspace Skill 不受影响 | 避免平台 Skill 抢占用户 Skill |
| 部分 provider 依赖 cwd 文件读取运行说明 | native 模式在内存里传递所选上下文：Codex 使用 developer instructions，其他 provider 放进任务 prompt | 不写文件仍能说明运行环境 |
| 取消旧 turn 后可能在目录释放前启动替代任务 | 明确的环境释放信号与上游 [#7818](https://github.com/multica-ai/multica/pull/7818) 的有界等待共同工作 | 避免取消/追加消息时丢会话，也不无限占用任务位 |
| Pi/OMP session 存在独立文件中，不应与 workdir 绑定 | 整合上游 [#7760](https://github.com/multica-ai/multica/pull/7760)，并为 transcript writer 做串行化 | cwd 改变或快速追加消息后仍保留 Pi 历史 |
| Codex rollout 与旧任务目录耦合 | 使用稳定 conversation store，并安全迁移已有 rollout | 切换到 native cwd 后仍可继续 Codex thread |
| Codex thread setup 比轻量 RPC 更容易超过 30 秒 | 整合上游 [#7790](https://github.com/multica-ai/multica/pull/7790)：轻量 RPC 保持 30 秒，`thread/start` 和 `thread/resume` 默认使用 60 秒 | 避免加载模型目录、MCP 或历史时误判启动失败 |
| 替代任务可能与旧进程同时写同一 session | Codex 和 Pi 按 conversation/session store 串行化 writer | 防止取消并追加消息时损坏 rollout 或 JSONL 序号 |
| Web 私聊的每个 turn 重复携带 audience、initiator、附件说明和平台简介 | 成功恢复同一 Codex thread 后只发新消息和本轮附件；冷启动回退仍保留完整说明 | 不浪费上下文，也不牺牲断线恢复安全性 |
| 重命名的本地构建可能不在子进程 PATH | daemon 把当前 CLI 的稳定别名加入任务 PATH | `multica` 命令保持可用，但不强迫 Agent 调用 |
| native cwd 与 daemon 可回收 workspace 如果没有所有权边界，GC 存在误判风险 | GC 修改前要求可验证的 Multica task ownership | 用户代码目录不会被当成已完成任务的临时目录 |
| 打开 Chat 后还要再点一次最近对话 | 前端自动打开最近会话 | 降低无意义操作 |
| 进行中任务的 token 尾量可能未进入统计 | 服务端统计补入 live usage tail | 正常展示本地 Agent 的 token 使用量 |

主要组合变更：

- [`17b27e12f`](https://github.com/mameikagou/multica/commit/17b27e12f)：增加 `full / minimal / off` 平台上下文模式；
- [`9120e85ad`](https://github.com/mameikagou/multica/commit/9120e85ad) + [`0cd106f242`](https://github.com/mameikagou/multica/commit/0cd106f242)：将 native cwd 从 Codex 泛化到全部 provider，并迁移可验证的旧 rollout；
- [`2f2070609`](https://github.com/mameikagou/multica/commit/2f2070609) + [#7818](https://github.com/multica-ai/multica/pull/7818)：同时使用精确释放信号和有界等待完成追加消息交接；
- [`bf4dd3a95`](https://github.com/mameikagou/multica/commit/bf4dd3a95) + [`149f0ef56`](https://github.com/mameikagou/multica/commit/149f0ef56)：串行化 Pi transcript 和 Codex conversation store writer；
- [`2a513594f`](https://github.com/mameikagou/multica/commit/2a513594f) + [`dfa277af7`](https://github.com/mameikagou/multica/commit/dfa277af7)：Codex Web 私聊恢复后只发增量 Prompt，并避免重复注入稳定 developer instructions；
- [`7a3702d51`](https://github.com/mameikagou/multica/commit/7a3702d51)：GC 修改目录前验证 task ownership；
- [`da985fec3`](https://github.com/mameikagou/multica/commit/da985fec3)：补齐进行中任务的 token 统计；
- [`33d7a6469`](https://github.com/mameikagou/multica/commit/33d7a6469)：保持版本化本地构建的 `multica` CLI 可用；
- [`c5f9479dc`](https://github.com/mameikagou/multica/commit/c5f9479dc)：Chat 入口自动打开最近会话。

## 平台上下文模式

### `minimal`：推荐

保留 Agent 无法自行推断的运行环境事实，以及当前任务可见 Skill 的中性清单；不注入 “Always Use”、issue workflow 或“匹配就必须调用 Skill”等策略。

```bash
multica --profile <profile> config set platform_context_mode minimal
```

这是本分支建议的网关粒度：能力仍然可见、命令仍然可用，但是否使用由模型判断或由用户主动提出。

### `off`：最小 Multica 影响

不提供 Multica runtime brief，并过滤服务端附带的 Multica 内置 Skill。provider 自己安装的 Skill、用户的 `~/.codex/skills`、`~/.agents/skills` 和 workspace Skill 不会因此被删除。

```bash
multica --profile <profile> config set platform_context_mode off
```

`off` 仍会保留任务传输必需的信息，例如当前用户消息、附件说明和发起人信息；它关闭的是本地 daemon 注入的运行时工作流，不是假装 Multica 不存在。

### `full`：上游兼容模式

保留原本完整的 Multica workflow、CLI 指令和 Skill 策略。代码默认值仍是 `full`，避免升级时无声改变其他安装者的行为；部署这个个人发行版时应显式选择 `minimal` 或 `off`。

## 部署方式 A：只替换本机网关（推荐）

这种方式继续使用 Multica Cloud 或现有服务端，只替换运行 Agent 的 CLI/daemon。它会获得 native cwd、上下文模式、会话交接、Pi/Codex 恢复和 CLI PATH 修复；不会自动获得本分支的前端或服务端页面改动。

### 1. 准备环境

- Go 1.26.x；
- 至少一个已经登录的 Agent CLI，例如 `codex`、`claude` 或 `cursor-agent`；
- 已经登录并能启动 daemon 的 Multica profile；
- 一个统一的代码根目录，例如 macOS 的 `/Users/<user>/code` 或 WSL 的 `/home/<user>/code`。

### 2. 获取个人维护分支

新设备：

```bash
git clone \
  --branch local/native-context-chat-handoff \
  --single-branch \
  https://github.com/mameikagou/multica.git
cd multica
```

已有 checkout：

```bash
git remote add fork https://github.com/mameikagou/multica.git 2>/dev/null || true
git fetch fork
git switch local/native-context-chat-handoff
git pull --ff-only fork local/native-context-chat-handoff
```

### 3. 构建版本化 CLI

macOS、Linux 或 WSL：

```bash
cd server

GATEWAY_COMMIT="$(git rev-parse --short=10 HEAD)"
GATEWAY_BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GATEWAY_BIN="$HOME/.local/bin/multica-gateway-$GATEWAY_COMMIT"

mkdir -p "$HOME/.local/bin"
go build \
  -ldflags "-X main.version=$GATEWAY_COMMIT -X main.commit=$GATEWAY_COMMIT -X main.date=$GATEWAY_BUILD_DATE" \
  -o "$GATEWAY_BIN" \
  ./cmd/multica

"$GATEWAY_BIN" version
ln -sfn "$GATEWAY_BIN" "$HOME/.local/bin/multica"
```

使用版本化文件而不是直接覆盖正在运行的可执行文件，可以避免 Desktop/daemon 仍持有旧 inode，也方便快速回滚。

### 4. 配置网关模式

下面以名为 `desktop-api.multica.ai` 的 profile 和 `/Users/luoyu32/code` 为例。其他设备替换成自己的 profile 与代码目录；使用默认 profile 时可以去掉 `--profile <profile>`。

```bash
multica --profile desktop-api.multica.ai config set \
  workspaces_root "$HOME/.local/share/multica/workspaces"

multica --profile desktop-api.multica.ai config set \
  native_workdir /Users/luoyu32/code

multica --profile desktop-api.multica.ai config set \
  platform_context_mode minimal

# 清理旧版 Codex-only 配置；新版本仍兼容它，但不建议同时保留两套键。
multica --profile desktop-api.multica.ai config set codex_native_workdir ""

# 清理旧的全局握手超时覆盖，启用新版本的 30s/60s 分层默认值。
# 若保留显式值（例如 2m），它会继续同时覆盖轻量 RPC 和 thread RPC。
multica --profile desktop-api.multica.ai config set codex_handshake_timeout ""
```

`workspaces_root` 继续存放 task logs、凭证、provider home 和 session scratch；它不再是 Agent 的 cwd。

### 5. 重启并验证

```bash
multica --profile desktop-api.multica.ai daemon restart
multica --profile desktop-api.multica.ai daemon status --output json
multica --profile desktop-api.multica.ai config show
```

然后分别启动一次 Codex、Claude 或 Cursor 对话并询问 `pwd`。预期结果：

1. cwd 是配置的 `native_workdir`；
2. `git status --short` 不出现 Multica 生成的 `AGENTS.md`、`.agent_context/`、`reasonix.toml` 或 `.cursor/mcp.json`；
3. `minimal` 模式下能看到 Skill 清单，但不会出现强制激活规则；
4. `multica` 命令仍在 PATH 中，只有任务确实需要时才调用。

再做一次会话连续性回归：在同一 Codex 对话里发出一条要求记住随机短语的消息，运行中停止并追加新消息，然后询问该短语。预期 `thread/resume` 在 60 秒预算内完成，历史仍在，并且 daemon 日志中没有 rollout ordinal 重复或连续的 30 秒 `thread/start` / `thread/resume` 超时。

### WSL 设备差异

WSL 使用同一套源码和构建命令，但 profile 与路径属于 WSL 自己，不要照搬 macOS 的绝对路径：

```bash
PROFILE="desktop-api.multica.ai"    # 替换为 WSL 实际 profile
CODE_ROOT="$HOME/code"

multica --profile "$PROFILE" config set \
  workspaces_root "$HOME/.local/share/multica/workspaces"
multica --profile "$PROFILE" config set native_workdir "$CODE_ROOT"
multica --profile "$PROFILE" config set platform_context_mode minimal
multica --profile "$PROFILE" config set codex_native_workdir ""
multica --profile "$PROFILE" config set codex_handshake_timeout ""
multica --profile "$PROFILE" daemon restart
multica --profile "$PROFILE" daemon status --output json
```

普通升级只需重启 Multica daemon，不需要重启整个 WSL。只有 WSL 自身或其 init/网络状态异常时，才从 Windows PowerShell 执行 `wsl --shutdown` 后重新进入发行版。

### 会话迁移预期

- Codex：首次切换会迁移可验证的旧 rollout，后续使用稳定 conversation store；
- Pi/OMP：session 是独立 JSONL 文件，可以跨 workdir 继续；
- 其他 cwd-keyed provider：从旧 Multica sandbox 第一次切到 native cwd 时，无法证明旧 session 可达就会诚实冷启动；记录新的稳定 cwd 后，后续 turn 才正常恢复。

## 部署方式 B：完整自建本分支

只有以下需求才需要构建整套服务：

- 使用本分支的 Chat 自动打开最近会话；
- 部署包含 migration 441 的 token live-tail 统计；
- 希望 Web、API 和 daemon 全部由自己维护。

```bash
git clone \
  --branch local/native-context-chat-handoff \
  --single-branch \
  https://github.com/mameikagou/multica.git
cd multica
make selfhost-build
```

`make selfhost-build` 会从当前 checkout 构建 backend 与 web，并启动本地 Docker Compose 栈。完整的密钥、邮件、域名和数据库说明沿用上游的 [Self-Hosting Guide](SELF_HOSTING.md)。每台实际运行 Agent 的机器仍需按照“部署方式 A”构建本分支 CLI，并设置 `native_workdir` 与 `platform_context_mode`。

## 升级

```bash
git fetch fork
git switch local/native-context-chat-handoff
git pull --ff-only fork local/native-context-chat-handoff
```

重新执行“构建版本化 CLI”，确认 `multica version` 后切换软链并重启 daemon。不要直接对个人分支执行 `git reset --hard origin/main`；需要吸收上游时，在单独分支完成 merge/rebase、测试后再合回个人维护分支。

## 回滚

先关闭本分支运行模式：

```bash
multica --profile <profile> config set native_workdir ""
multica --profile <profile> config set platform_context_mode full
```

然后把 `~/.local/bin/multica` 软链切回之前保留的二进制，或重新安装官方 CLI，最后执行：

```bash
multica --profile <profile> daemon restart
```

版本化二进制和独立 `workspaces_root` 让这个过程不需要删除用户代码或任务历史。

## 明确的取舍

- 所有普通任务共享同一个 `native_workdir`，本分支不会额外加写锁。这是有意设计：由用户决定哪些会话可以并发，而不是让网关猜测任务是否冲突。
- Agent 以 daemon 所属系统用户的权限直接操作代码目录。这个分支优化的是工作流边界，不提供额外文件系统沙箱。
- `minimal` 降低平台提示词和误触发概率，但模型仍可能根据 Skill 自身过宽的描述主动选择它；真正零平台 Skill 使用应选择 `off`，或收紧相应 Skill 自身的说明。
- 连接官方 Multica Cloud 时，本地 fork 只能控制 daemon 侧行为；服务端和前端改动必须等上游部署，或使用完整自建模式。

## 支持的 Agent/Harness

本分支沿用上游 provider 支持，包括 Codex、Claude Code、Cursor Agent、Pi、Oh-My-Pi、OpenCode、OpenClaw、Hermes、Kimi、Kiro、Qwen、QwenPaw、Reasonix、DeepSeek Harness、Grok、Trae、CodeBuddy、Copilot、Antigravity、Qoder、MiniMax Code、Dim、ZeroClaw 等。

provider 安装与认证方式继续参考上游的 [runtime documentation](https://multica.ai/docs/providers)。这个 fork 不替代 provider，也不代理模型请求；它只聚合和调度用户已经安装好的 Agent/Harness。

## 上游与许可证

上游项目：[multica-ai/multica](https://github.com/multica-ai/multica)。通用修复仍适合独立提交上游；纯个人工作流取舍只维护在本分支。

许可证沿用仓库中的 [LICENSE](LICENSE)。
