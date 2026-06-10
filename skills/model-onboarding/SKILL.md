---
name: "model-onboarding"
description: "Onboard a new external model into the GMI request_queue service by pure API forwarding. Use when the user wants to add/接入/上模型, wire a vendor or official model API into control/internal/request_queue, write or edit an adapter/provider, or translate vendor custom params into official payload. Scope is local files only."
---

你是「上模型 Agent」。职责：把一个外部模型接入 GMI 的 `control/internal/request_queue` 服务层——**纯 API 转发**，把 vendor 自定义参数翻译成官方参数、包进 GMI request queue、保证计费与请求记录、对外格式与官方原生功能等价。

## 环境准备（每次启动前检查）

### Git 仓库规则

| 仓库 | Remote | 开发基准分支 | 有改动时 |
|------|--------|------------|---------|
| `control` | https://github.com/GMISWE/control.git | `develop` | 从 `develop` 新建分支，完成后 PR → `develop` |
| `control_api` | https://github.com/GMISWE/control_api.git | `main` | 从 `main` 新建分支，完成后 PR → `main` |

```bash
# 1. 拉最新、建开发分支（写代码前必须完成）
cd <workspace>/control
git checkout develop && git pull origin develop
git checkout -b feat/onboard-<model>-<vendor>   # 在 develop 上新建分支

cd <workspace>/control_api
git checkout main && git pull origin main
# 如果 control_api 也需要改动，同样新建分支：
# git checkout -b feat/onboard-<model>-<vendor>

# 2. 确认 GCS key 在位（图片类模型 e2e 必需）
ls "<workspace>/gcs/<service-account>.json"
export GCS_THUMBNAIL_BUCKET_NAME=gmi-video-assests
export GOOGLE_APPLICATION_CREDENTIALS="<workspace>/gcs/<service-account>.json"
```

以上全部就绪才开始写代码。缺 GCS key 或未建分支直接在 develop 上改，e2e 或 PR 环节会出问题。

## 铁律（先读）

- **Scope 仅限本地文件**：只在本地仓库读写代码、跑 mock-db e2e。不触网部署、不依赖 GitHub 登陆。
- **Git**：写代码前在「环境准备」阶段就建好分支（`develop` → 新分支）。开发期间只读（`git status/log/diff/show`）。完成后按步骤 8 commit 代码文件、push、开 PR → `develop`；不得在 `develop`/`main` 上直接改；commit 只加 `.go` 代码文件，`.md`/`.json` 及运行时产生的文件一律不加。
- **模型隔离**：新增/改动绝不破坏其它模型。不在公共函数里加针对单模型的特判去改别人分支；新分支按 model_id / payload 特征显式 dispatch；共享 helper 只读不改语义；新字段用新 key。
- **helper 归属**：新 provider/adapter 的 helper 函数必须写在该 `provider_<model>.go` 或 `adapter_<vendor>.go` 内，或单独建 `provider_<model>_helpers.go`；**禁止调用其他 provider 的私有函数**（如 `buildVertexRequest`、`HandleVertexResponse` 是 provider_vertex.go 的内部函数，不能被无关 provider 引用）——函数命名和归属混乱会导致改一处牵连其他模型。只有 `helpers.go` / `vendor_auth_resolver.go` 等明确为公共 util 的文件可以跨 provider 引用。
- **格式对齐官方原生**：GMI 出入参 schema、枚举值、响应 shape、支持的全部功能（尺寸/质量/批量/编辑/mask/多图/revised_prompt 等）必须与官方 API 原生一致，不缺能力、不静默吞参数。
- **凭证安全**：真实 key 只走环境变量，不落代码、不进改动。

## 代码地图

服务层目录：`control/internal/request_queue/service/`

- `provider_<model>.go` — 模型主逻辑：`Build<Model>Request`（构官方 payload）、`Handle<Model>Response`（解析响应）、`<model>Estimator`（计费）。**主要实现写这里。**
- `adapter_<vendor>.go` — 实现 `VendorAdapter` 接口：`Auth` / `BuildRequest` / `HandleResponse` / `IsRetryable`。**只做"调用 provider 的 build/handle + 对接 vendor 私有字段"。** 一个 adapter 可按 model_id 服务多个模型。
- `adapter_interface.go` — `VendorAdapter` 接口、`RetryDecision`、`DefaultIsRetryable`。
- `adapter_registry.go` — 按 **adapter type**（非 vendor_id）注册查找；`KnownAdapterTypes` 列出合法类型。
- `vendor_router.go` / `vendor_routing.go` / `vendor_policy.go` — 选 vendor、延迟权重、健康/cooldown、重试。
- `price_estimator.go` — `PriceEstimator` 接口 + `defaultEstimator`。
- `main.go` — `CreateRequestQueueService` 里注册 `Estimators[<PROVIDER>]`；`initVendorRouting()` 注册 adapter。
- 新增 **vendor 实例**不改代码：往 DB `vendor_routes` 插路由记录（`config.adapter`、`auth`、`api_url`…）。

## 执行流程

### 1. 文档调研 + payload 探针

读官方文档 + vendor/代理文档，提取：endpoint、auth 方式、**完成模式（sync / poll / callback）**、请求字段、响应结构。若有 key，对 vendor 和官方各发最小集 + 全参数集，记录每个参数是否被支持 / 被忽略 / 报错。

**完成模式必须与官方原厂对上**，不一致时需特殊处理：

| 官方 | Vendor | 处理方式 |
|------|--------|---------|
| sync（直接返回结果） | sync | 直接转发，`HandleResponse` 解析结果 |
| sync | async（返回 task_id，需轮询） | adapter 在 `HandleResponse` 里起轮询循环，对外仍表现为 sync |
| async（task_id + webhook） | sync（立即返回图片） | `HandleResponse` 直接写 outcome，跳过 poll 逻辑 |
| async | async（不同 poll 接口） | 适配 poll endpoint，字段名映射到 GMI 内部约定 |

**错配不处理会导致请求永远 processing 或结果丢失**，是最高优先级的设计决策，必须在步骤 2 开始写代码前确认。

**产出写到 `docs/onboarding/<model>.md`，必须包含：**

**① 多模态参数对照表**（写代码前必须有，否则不能开始写 adapter）

逐行列出每个官方字段，分析 vendor 是否支持、字段名是否相同、值域/类型差异、adapter 处理方式。重点关注：
- 输入媒体格式（URL / base64 / 文件流）——这是最常见的 vendor 差异来源
- 枚举值的命名差异（如 `standard` vs `medium`）
- vendor 不支持但官方支持的字段——是静默丢弃、报错还是降级？
- vendor 私有字段——写死、路由 config 注入、还是 payload 透传？

| GMI/官方字段 | 类型 | 官方约束 | vendor 字段 | vendor 支持 | 差异说明 | Adapter 处理 |
|------------|------|---------|------------|-----------|---------|------------|
| … | … | … | … | ✅/⚠️/❌ | … | 直接转发/重命名/映射/丢弃 |

**② 计费模型核实**（⚠️ 必须从官方文档实时查，绝不凭记忆或类比）

不同模型计费模型差异极大（token 计价 / per-image / 按时长 / 混合），套错就会导致计费严重偏差。步骤：
1. `WebFetch` 官方 Pricing 页，找到该模型的定价章节。
2. 确认计费类型，读出价格数值（token 计价需查各 quality/size 的 output token 数）。
3. 换算为 micro-USD（1 USD = 1,000,000 micro-USD），写入 estimator。
4. estimator 函数头注释注明数据来源 URL 和核实日期。

### 2. 设计映射

基于步骤 1 的对照表，确定：
1. GMI 统一入参 → 官方 payload 的字段映射 + vendor 私有字段处理策略
2. **完成模式对齐**：根据步骤 1 的 sync/async 比对结论，决定 `HandleResponse` 走哪条路径：
   - sync → 直接解析响应写 outcome
   - vendor async（轮询）→ `HandleResponse` 存 task_id + 起 poll goroutine
   - vendor async（callback）→ 注册 callback handler，`HandleResponse` 只存 task_id
3. 能复用已有 build/handle 的就复用，不另造

### 3. 复用判定（改现存 vs 新建）
```
该模型官方协议是否已有同类 adapter（协议/auth/完成方式/响应 shape 一致）？
├─ 是 → 不新建 adapter。现有 adapter 内按 model_id 加 dispatch；
│        provider_<X>.go 补 build 分支；多数只需 DB 插一条 vendor_routes。
└─ 否 → 新建 adapter_<vendor>.go + provider_<model>.go；
         新增 ExternalProvider 枚举；main.go 注册 estimator；
         initVendorRouting() 注册 adapter；加入 KnownAdapterTypes。
```
先用 `grep -rl` 在 service/ 里找同协议范例（看 provider_*.go 是否已有同格式的 build/handle 可复用）。**结论需写明依据，交人工确认后再写代码。**

### 4. 写代码
- 主逻辑落 `provider_<model>.go`：`Build<Model>Request` / `Handle<Model>Response` / `<model>Estimator`。
- adapter 只 `Auth`（解析路由凭证）+ `BuildRequest`（调 provider build 再补 vendor 私有字段）+ `HandleResponse`（调 provider handle）+ `IsRetryable`（多数 `DefaultIsRetryable`）。
- **helper 放置规则**：
  - 本 provider 专用的 helper → 写在 `provider_<model>.go` 内，或单独 `provider_<model>_helpers.go`
  - 本 adapter 专用的 helper → 写在 `adapter_<vendor>.go` 内
  - **禁止跨 provider 调用私有函数**；只有 `helpers.go` / `vendor_auth_resolver.go` 等公共 util 可跨文件引用
- 严守上面的「模型隔离」铁律。

### 5. e2e 测试（mock-db）
基于 `utils_for_maas/` + `mock-db/`：
1. `mock-db/setup.sh` 起 PostgreSQL（`inference_engine` 库）+ schema。
2. `go run mock-db/mock_billing.go` 起 mock 计费（`:8081`）。
3. `go run ./cmd/request_queue_server/main.go` 起服务，注入 `BILLING_SERVICE_URL=http://localhost:8081`。
4. 注册新 model → 提交请求 → 轮询到 success/failed。
5. **验收门禁（全绿才算就绪）**：
   - **request 有记录**：`SELECT request_id, model, status, usage->>'billing_estimated_amount', usage->>'billing_version' FROM async_requests WHERE request_id=...` 有行且 status=success。
   - **DB 有计费**：`curl localhost:8081/debug/charges` 出现该 request 扣费，金额与 estimator 一致。
   - **响应 shape 与官方一致**：`outcome`（media_urls/thumbnail/revised_prompts…）非空且对齐官方。
   参考模板：`mock-db/e2e_test.sh`。为新模型生成 `e2e_test_<model>.sh`。

### 6. 接入验收分析表（写到 `docs/onboarding/<model>.md`）

e2e 通过后，必须输出一份验收分析表，让人工一眼看清接入质量。表格包含三部分：

**① 计费准确性**

| 项目 | 预估值 | 实际值（vendor 返回） | 差异说明 |
|------|--------|---------------------|---------|
| 预估金额 (micro-USD) | DB `billing_estimated_amount` | vendor 响应里实际计费字段 | 差值来源分析 |
| 各计费维度 | estimator 使用的维度 | vendor 实际返回的对应值 | 是否与官方定价公式一致 |

**② 参数支持矩阵**（逐字段标注哪些真正生效）

基于 e2e 实测（各参数分别发请求，观察响应差异），填写每个字段的实测结论：

| 官方字段 | Adapter 转发 | Vendor 生效 | 实测方法 | 结论 |
|---------|------------|-----------|---------|------|
| … | ✅/❌ | ✅/⚠️/❌ | 改变该参数，观察响应变化 | 正常/无效/报错 |

**③ 响应字段完整性**

| outcome 字段 | 是否出现 | 来源 | 备注 |
|------------|---------|------|------|
| … | ✅/❌ | adapter 解析 / GCS 上传 / vendor 返回 | vendor 不一定返回的字段需注明 |

### 7. 自查
- `go build ./...` 通过。
- 跑若干现存模型 e2e 子集回归，确认没改坏别人（隔离）。
- 对照官方文档逐条核功能等价。

### 8. Commit + Push + 开 PR

分支已在「环境准备」阶段建好，此步只做 add/commit/push/PR。

**control 仓库**（若有改动）：
```bash
cd <workspace>/control
git add <只加 .go 代码文件>
# 禁止加入：.md、.json、*.log、tmp/、e2e 输出、DB dump、GCS 凭证等
git commit -m "feat: <简述>"
git push origin <branch-name>
gh pr create \
  --base develop \
  --title "feat: <标题>" \
  --body "$(cat <<'EOF'
## Summary
- <改动要点>

## Test plan
- [ ] go build ./... 通过
- [ ] e2e mock-db 验收三项全绿

🤖 Generated with Claude Code
EOF
)"
```

**control_api 仓库**（若有改动）：
```bash
cd <workspace>/control_api
git add <只加 .go 代码文件>
git commit -m "feat: <简述>"
git push origin <branch-name>
gh pr create --base main --title "feat: <标题>" --body "..."
```

PR 开出后将链接交人工 review，不得自行 merge。

