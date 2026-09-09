<div align="center">

# Multica Gateway Fork

**一个不接管用户工作流的多 Agent 聚合网关。**

[个人维护分支](https://github.com/mameikagou/multica/tree/local/native-context-v0.4.42) ·
[上游项目](https://github.com/multica-ai/multica) ·
[上游文档](https://multica.ai/docs)

</div>

> [!IMPORTANT]
> 这是基于官方最新发布版 **v0.4.42** 的个人发行分支（`local/native-context-v0.4.42`）。它保留 Multica 优秀的传输、路由、运行时注册、聊天界面、执行日志和用量统计能力，同时尽量不接管 Codex、Claude Code、Cursor、Pi、Antigravity 等 Agent/Harness 原本的工作方式。

## 这条维护分支的价值

这是一个长期维护的 **Agent 网关发行版**。它保留 Multica 作为多端入口和运行时调度器的价值，同时把工作目录、会话、Prompt、Skill 和代码目录的最终控制权交还给用户和 Agent。它的价值来自一套可长期使用的边界，不靠堆叠开关。

> [!NOTE]
> **上游默认提供受管任务工作流。** 平台会向运行目录写入 sidecar垃圾文件，并向模型注入 Workflow 和 Skill 激活规则。这套设计适合AI native 程序不高，希望通过 Multica 推动 AI 更全面普及的团队，但对已有成熟本地工作流的用户是一种打扰。
>
> GPT-5.6 Sol、Claude Fable 5 和 Claude Opus 5 这类 SOTA（当前能力最强的一档）模型已经能自行规划、调用工具、拆分任务并根据执行结果调整做法。继续强加一套固定 Workflow，往往会变成 Superpowers skill一样的枷锁。它迫使模型在每个 turn 重复扫描和执行已经具备的能力，额外消耗 token，也会降低实际任务表现：过度规划、调用无关 Skill、重复验证、陷入循环，最后把简单修改做得更慢、更复杂。[用户删除 Superpowers 后，异常 token 消耗停止](https://www.reddit.com/r/OpenaiCodex/comments/1v3mt8k/burned_4_of_my_weekly_limit_just_by_pushing_and/)；
[GPT-5.6 Codex 用户讨论：多人因 token 消耗、过度思考和质量下降而停用 Superpowers](https://www.reddit.com/r/codex/comments/1uzbpec/does_superpowers_suck_with_56_and_just_eat_tokens/)。

- **官方客户端，自维护网关。** Web、Desktop 和 Mobile 可以继续跟随官方版本；本机只需替换同一个 Go CLI/daemon，不需要 fork 整个客户端就能获得核心体验。
- **Workflow 和 Skill 不再默认接管模型。** 上游完整 Prompt 会重复强调 issue/comment 工作流、CLI 使用和 Skill 激活规则；“匹配就调用”会让模型过度敏感，容易启动与当前任务无关的 Skill，同时持续消耗上下文。本分支的 `minimal` 只保留必要环境事实和中性 Skill 清单，**可见不等于必须触发**；`off` 移除 Multica 运行时工作流和服务端 Skill，`full` 才保留官方完整行为。
- **Multica 不再向代码目录写入 sidecar。** 上游运行模式可能写入或改写 `AGENTS.md`、`CLAUDE.md`、`.agent_context/`、`reasonix.toml`、`.cursor/mcp.json` 等运行说明和项目 MCP 配置。native 模式不向 cwd 写这些 Multica runtime sidecar；日志、凭证、provider home 和 task scratch 全部保持在独立的私有状态目录。
- **连续会话，不用平台工作流换取历史。** Codex thread 和 Pi/OMP session 不再因 workdir 变化、取消 turn 或追加消息轻易冷启动；Web 私聊恢复后只发送新增消息，不在每个 turn 重放稳定的平台套话。
- **通用修复回馈上游，个人取舍留在 fork。** 可复用的协议、会话和 CLI 修复会拆成独立 PR 提交给 Multica；“网关而非工作流”这种明确带有个人偏好的组合仍由本分支维护。

## 已被上游合并的贡献

截至 2026-09-09，[`mameikagou`](https://github.com/mameikagou) 向 [`multica-ai/multica`](https://github.com/multica-ai/multica) 提交的以下 7 个 PR 已全部正式合并（日期按 GitHub UTC）：

| PR | 上游获得的能力 | 合并日期 |
| --- | --- | --- |
| [#7331 · daemon server URL override](https://github.com/multica-ai/multica/pull/7331) | 自部署环境可用 `MULTICA_DAEMON_SERVER_URL` 分离公网 webhook 地址与 daemon 私有出口 | 2026-08-24 |
| [#7557 · remove obsolete autopilot priority flag](https://github.com/multica-ai/multica/pull/7557) | 移除已废弃但仍被 CLI 静默接受的 autopilot priority 参数，让 CLI、API 与产品语义一致 | 2026-08-26 |
| [#7756 · explicit Codex Standard speed](https://github.com/multica-ai/multica/pull/7756) | 区分“继承本地配置”、“明确 Standard”与“Fast”，支持本地默认 Fast 时主动切回 Standard | 2026-09-01 |
| [#7760 · Pi session continuity](https://github.com/multica-ai/multica/pull/7760) | Pi/OMP 使用独立 JSONL session 时不再被 workdir 变化错误阻断恢复 | 2026-08-31 |
| [#7790 · Codex thread handshake budget](https://github.com/multica-ai/multica/pull/7790) | 为 `thread/start` / `thread/resume` 设置独立的 60 秒默认预算，轻量 RPC 继续保持 30 秒 | 2026-08-31 |
| [#7798 · require proven task ownership before GC mutation](https://github.com/multica-ai/multica/pull/7798) | GC 修改磁盘前必须先用 `.task_owner` 证明目录由 Multica 创建，普通目录不会再因为够旧或缺少完成信息就被递归删除。这项修复来自一次误删 20 GB 以上文件的真实事故，把“无法确认归属”改成保留目录，而不是猜测删除。 | 2026-09-01 |
| [#8200 · classify concurrent request rejections](https://github.com/multica-ai/multica/pull/8200) | 将带有 `concurrent request limit` 的 provider 403 正确识别为临时容量限制，不再误导用户重新登录或把正常会话判成上下文溢出 | 2026-09-09 |

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
| provider 偶发返回 `403 concurrent request limit` | 整合上游 [#8200](https://github.com/multica-ai/multica/pull/8200) 的准确分类；上游只修正提示，本分支额外按现有任务次数自动重发一次 | 不再误导用户重新认证，短暂并发拒绝通常也不需要手动重发消息 |
| 打开 Chat 后还要再点一次最近对话 | 前端自动打开最近会话 | 降低无意义操作 |
| 进行中任务的 token 尾量可能未进入统计 | 服务端统计补入 live usage tail | 正常展示本地 Agent 的 token 使用量 |
| Antigravity 运行中存在陈旧 active steps、临时认证超时或过早认定结束 | 过滤已失效 active steps，遇瞬时认证超时自动重试，强制等待最新响应完全完成并保留完整回复；完整记录 input/output/cache Token 用量 | 保障 Google Antigravity 作为主力 Agent 时的稳定执行与真实用量统计 |
| Claude 在执行定时循环（schedule/cron/timer）时，终态结果到达可能导致会话提前中断或死锁 | 支持原生循环跨终端结果持续活跃，定时唤醒后保留终态结果，精准累积多轮循环 Token | 解决 Claude 自动化轮询和长时间后台任务的会话保持与精准计费 |
| 任务执行偶发因环境瞬时抖动失败，或后台定时等待缺乏守护 | 任何执行失败在本地自动重试一次；定时调度等待（scheduled waits）与看门狗保障（watchdog）深度复合 | 显著提升任务执行韧性，长等待与定时轮询任务更稳健 |

主要组合变更：

- [`a9a546311`](https://github.com/mameikagou/multica/commit/a9a546311)：增加 `full / minimal / off` 平台上下文模式；
- [`89998e620`](https://github.com/mameikagou/multica/commit/89998e620)：服务端统计补入 live usage tail（进行中任务的 Token 统计）；
- [`fa206c6a2`](https://github.com/mameikagou/multica/commit/fa206c6a2)：保持版本化本地构建的 `multica` CLI 在任务 PATH 中可用；
- [`a3f19fe8f`](https://github.com/mameikagou/multica/commit/a3f19fe8f)：串行化 Codex session store writers，防止并发冲突；
- [`90cfcc23a`](https://github.com/mameikagou/multica/commit/90cfcc23a)：Codex Web 私聊恢复后只发增量 Prompt，避免重复注入稳定 developer instructions；
- [`c4293efb3`](https://github.com/mameikagou/multica/commit/c4293efb3) + [`526d17094`](https://github.com/mameikagou/multica/commit/526d17094) + [`2ff7a1086`](https://github.com/mameikagou/multica/commit/2ff7a1086) + [`08c4197f2`](https://github.com/mameikagou/multica/commit/08c4197f2) + [`e13003fa5`](https://github.com/mameikagou/multica/commit/e13003fa5) + [`126ef60e3`](https://github.com/mameikagou/multica/commit/126ef60e3)：Antigravity 全面适配：完整 Token 用量记录、响应完成等待、陈旧步骤过滤、瞬时认证超时自动重试与回复保护；
- [`1d9c735ed`](https://github.com/mameikagou/multica/commit/1d9c735ed) + [`4db39b9b6`](https://github.com/mameikagou/multica/commit/4db39b9b6) + [`ca688aa73`](https://github.com/mameikagou/multica/commit/ca688aa73) + [`8e920fb51`](https://github.com/mameikagou/multica/commit/8e920fb51) + [`42bce34c5`](https://github.com/mameikagou/multica/commit/42bce34c5) + [`b2a004904`](https://github.com/mameikagou/multica/commit/b2a004904)：Claude 原生循环调度支持（Scheduled Loops & Native Loops），跨结果保持活跃，唤醒后保留终态结果，精准累积 Token 用量；
- [`ab41c5523`](https://github.com/mameikagou/multica/commit/ab41c5523)：将定时等待（scheduled waits）与看门狗超时监控（watchdog safeguards）深度复合；
- [`8806ed0c1`](https://github.com/mameikagou/multica/commit/8806ed0c1)：本地执行失败自动重试一次（retry every execution failure once locally）。

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
  --branch local/native-context-v0.4.42 \
  --single-branch \
  https://github.com/mameikagou/multica.git
cd multica
```

已有 checkout：

```bash
git remote add fork https://github.com/mameikagou/multica.git 2>/dev/null || true
git fetch fork
git switch local/native-context-v0.4.42
git pull --ff-only fork local/native-context-v0.4.42
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

使用版本化文件可以避免直接覆盖正在运行的可执行文件，也能防止 Desktop/daemon 继续持有旧 inode，方便快速回滚。

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
  --branch local/native-context-v0.4.42 \
  --single-branch \
  https://github.com/mameikagou/multica.git
cd multica
make selfhost-build
```

`make selfhost-build` 会从当前 checkout 构建 backend 与 web，并启动本地 Docker Compose 栈。完整的密钥、邮件、域名和数据库说明沿用上游的 [Self-Hosting Guide](SELF_HOSTING.md)。每台实际运行 Agent 的机器仍需按照“部署方式 A”构建本分支 CLI，并设置 `native_workdir` 与 `platform_context_mode`。

## 升级

```bash
git fetch fork
git switch local/native-context-v0.4.42
git pull --ff-only fork local/native-context-v0.4.42
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

- 所有普通任务共享同一个 `native_workdir`，本分支不会额外加写锁。用户决定哪些会话可以并发；网关不猜测任务是否冲突。
- Agent 以 daemon 所属系统用户的权限直接操作代码目录。这个分支优化的是工作流边界，不提供额外文件系统沙箱。
- `minimal` 降低平台提示词和误触发概率，但模型仍可能根据 Skill 自身过宽的描述主动选择它；真正零平台 Skill 使用应选择 `off`，或收紧相应 Skill 自身的说明。
- 连接官方 Multica Cloud 时，本地 fork 只能控制 daemon 侧行为；服务端和前端改动必须等上游部署，或使用完整自建模式。

## 支持的 Agent/Harness

本分支沿用上游 provider 支持，包括 Codex、Claude Code、Cursor Agent、Pi、Oh-My-Pi、OpenCode、OpenClaw、Hermes、Kimi、Kiro、Qwen、QwenPaw、Reasonix、DeepSeek Harness、Grok、Trae、CodeBuddy、Copilot、Antigravity、Qoder、MiniMax Code、Dim、ZeroClaw 等。

provider 安装与认证方式继续参考上游的 [runtime documentation](https://multica.ai/docs/providers)。这个 fork 不替代 provider，也不代理模型请求；它只聚合和调度用户已经安装好的 Agent/Harness。

## 上游与许可证

上游项目：[multica-ai/multica](https://github.com/multica-ai/multica)。通用修复仍适合独立提交上游；纯个人工作流取舍只维护在本分支。

许可证沿用仓库中的 [LICENSE](LICENSE)。
