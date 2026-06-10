# gpt-image-2 接入文档

## 基本信息

| 项目 | 值 |
|------|-----|
| 官方 Provider | OpenAI GPT Image 2 |
| 模型 ID | gpt-image-2 |
| GMI model_id | gpt-image-2-generate / gpt-image-2-edit |
| ExternalProvider | gpt-image (GPT_IMAGE_PROVIDER) |
| 完成模式 | Sync（直接返回图片数据） |
| Estimator | gptImageEstimator（provider_gpt_image.go） |

## Vendor: shengsuanyun（胜算云）

| 项目 | 值 |
|------|-----|
| 官网 | https://www.shengsuanyun.com/ |
| API 文档 | https://docs.router.shengsuanyun.com |
| Base URL | https://router.shengsuanyun.com |
| Auth | Authorization: Bearer {api_key} |
| adapter type | shengsuanyun |

### Endpoints

| 模型路径 | Method | Path |
|---------|--------|------|
| generate | POST | /v1/images/generations |
| edit | POST | /v1/images/edits |

### 特性说明

- **接受 URL 作为 image 输入**：edit 端点支持直接传 URL，不需要 base64（这是与 apihub_oai 的关键差异）
- **response_format=url**：可请求返回 URL 而非 b64_json
- **地域限制**：从境外 IP 直接请求会返回 405；服务需从中国大陆/境内节点访问

### 请求格式

**Generate:**
```json
{
  "model": "gpt-image-2",
  "prompt": "...",
  "n": 1,
  "size": "1024x1024",
  "quality": "standard",
  "background": "auto",
  "output_format": "png",
  "response_format": "url"
}
```

**Edit:**
```json
{
  "model": "gpt-image-2",
  "prompt": "...",
  "image": "https://gcs-url/...",
  "mask": "https://gcs-url/...",
  "n": 1,
  "size": "1024x1024",
  "response_format": "url"
}
```

### 响应格式（OpenAI 标准）

```json
{
  "created": 1749520000,
  "data": [
    {
      "url": "https://...",
      "b64_json": null,
      "revised_prompt": "..."
    }
  ]
}
```

## 字段支持矩阵

| GMI 字段 | 官方字段 | shengsuanyun | 备注 |
|----------|---------|-------------|------|
| prompt | prompt | ✅ | 必填 |
| n | n | ✅ | 默认 1 |
| size | size | ✅ | 1024x1024 / 1024x1792 / 1792x1024 |
| quality | quality | ✅ | standard / hd / auto |
| background | background | ✅ | auto / opaque（不支持 transparent） |
| output_format | output_format | ✅ | png / jpeg / webp |
| output_compression | output_compression | ✅ | 0-100，jpeg/webp 有效 |
| moderation | moderation | ✅ | low / auto |
| input_fidelity | input_fidelity | ✅ | edit 专用 |
| image | image | ✅ URL | edit 专用，接受 URL 字符串 |
| mask | mask | ✅ URL | edit 专用，接受 URL 字符串 |

## 计费

gpt-image-2 为 **token 计价**（OpenAI 官方定价，2026-06 核实）：
- 输入 token（文本 + 图片）：$8.00 / 1M tokens
- 输出图像 token：$30.00 / 1M tokens

| quality | 尺寸 | 单张价格 | micro-USD |
|---------|------|---------|-----------|
| low | 1024×1024 | $0.006 | 6,000 |
| low | 1024×1536 / 1536×1024 | $0.005 | 5,000 |
| medium / auto | 1024×1024 | $0.053 | 53,000 |
| medium / auto | 1024×1536 / 1536×1024 | $0.041 | 41,000 |
| high | 1024×1024 | $0.211 | 211,000 |
| high | 1024×1536 / 1536×1024 | $0.165 | 165,000 |

## GMI 模型注册

### gpt-image-2-generate

```json
{
  "model_id": "gpt-image-2-generate",
  "model_type": "image",
  "external_provider": "gpt-image",
  "external_api_url": "https://router.shengsuanyun.com",
  "external_api_endpoint": "/v1/images/generations",
  "internal_parameters": {"model": "gpt-image-2"},
  "parameters": [
    {"name": "prompt", "type": "string", "required": true},
    {"name": "n", "type": "number", "required": false, "default_value": 1},
    {"name": "size", "type": "enum", "required": false, "values": ["1024x1024","1024x1792","1792x1024"]},
    {"name": "quality", "type": "enum", "required": false, "values": ["standard","hd","auto"]},
    {"name": "background", "type": "enum", "required": false, "values": ["auto","opaque"]},
    {"name": "output_format", "type": "enum", "required": false, "values": ["png","jpeg","webp"]},
    {"name": "output_compression", "type": "number", "required": false},
    {"name": "moderation", "type": "enum", "required": false, "values": ["low","auto"]}
  ]
}
```

### gpt-image-2-edit

```json
{
  "model_id": "gpt-image-2-edit",
  "model_type": "image",
  "external_provider": "gpt-image",
  "external_api_url": "https://router.shengsuanyun.com",
  "external_api_endpoint": "/v1/images/edits",
  "internal_parameters": {"model": "gpt-image-2"},
  "parameters": [
    {"name": "prompt", "type": "string", "required": true},
    {"name": "image", "type": "image", "required": true, "max_value": 1},
    {"name": "mask", "type": "image", "required": false, "max_value": 1},
    {"name": "n", "type": "number", "required": false, "default_value": 1},
    {"name": "size", "type": "enum", "required": false, "values": ["1024x1024","1024x1792","1792x1024"]},
    {"name": "quality", "type": "enum", "required": false, "values": ["standard","hd","auto"]},
    {"name": "background", "type": "enum", "required": false, "values": ["auto","opaque"]},
    {"name": "output_format", "type": "enum", "required": false, "values": ["png","jpeg","webp"]},
    {"name": "output_compression", "type": "number", "required": false},
    {"name": "moderation", "type": "enum", "required": false, "values": ["low","auto"]},
    {"name": "input_fidelity", "type": "enum", "required": false, "values": ["low","high","auto"]}
  ]
}
```

## Vendor Route 注册（via API）

```json
{
  "model_id": "gpt-image-2-generate",
  "vendor_id": "shengsuanyun-gpt-image",
  "weight": 100,
  "enabled": true,
  "config": {
    "adapter": "shengsuanyun",
    "api_url": "https://router.shengsuanyun.com",
    "api_endpoint": "/v1/images/generations",
    "auth": {
      "api_key": "<SHENGSUANYUN_API_KEY>"
    }
  }
}
```

（edit 同理，`api_endpoint` 改为 `/v1/images/edits`，或在 generate route config 里加 `edit_api_endpoint`）

---

## 接入验收分析表（shengsuanyun, e2e mock 测试, 2026-06-10）

### ① 计费准确性

| 项目 | 预估值 | 实际值（vendor 返回） | 差异说明 |
|------|--------|---------------------|---------|
| 预估金额 (micro-USD) | 6,000 | 6,384 | +384 = input 48 text tokens × 8 micro-USD |
| input_tokens | — | 48 | 纯文本 prompt |
| output_tokens | — | 200 | low quality 1024×1024 |
| 计费公式 | quality/size 查表 | 48×8 + 200×30 = 6,384 | 与官方 token 定价一致 |

预估偏差 6.4%，来源是 estimator 未计 input text token（$8/1M 较小，可接受）。

### ② 参数支持矩阵（shengsuanyun）

| 官方字段 | Adapter 转发 | Vendor 生效 | 实测状态 | 备注 |
|---------|------------|-----------|---------|------|
| `prompt` | ✅ | ✅ | 已验证 | 图片随 prompt 变化 |
| `model` (internal) | ✅ | ✅ | 已验证 | internal_parameters 注入 |
| `response_format` | ✅ 固定 `url` | ✅ | 已验证 | adapter 写死 |
| `size` | ✅ | 待实测 | 境外 IP geo-restricted | 逻辑已实现 |
| `quality` | ✅ | 待实测 | 同上 | estimator 按此计价 |
| `n` | ✅ | 待实测 | 同上 | response 多 item 路径已处理 |
| `background` | ✅ | 待实测 | 同上 | 不传 transparent（adapter 无需过滤） |
| `output_format` | ✅ | 待实测 | 同上 | |
| `output_compression` | ✅ | 待实测 | 同上 | |
| `moderation` | ✅ | 待实测 | 同上 | |
| `image` (edit) | ✅ URL pass-through | 待实测 | shengsuanyun 接受 URL | 不做 base64 转换 |
| `mask` (edit) | ✅ URL pass-through | 待实测 | 同上 | |
| `input_fidelity` (edit) | ✅ | 待实测 | 同上 | |

### ③ 响应字段完整性

| 字段 | 出现在 outcome | 来源 | 备注 |
|------|--------------|------|------|
| `media_urls` | ✅ | GCS 上传后 URL | |
| `thumbnail_image_url` | ✅ | media_urls[0] | |
| `revised_prompts` | ✅ | vendor data[].revised_prompt | mock 有，真实 vendor 待确认 |
| `request_usage.input_tokens` | ✅ | vendor usage | mock 有，真实 vendor 待确认 |
| `request_usage.output_tokens` | ✅ | vendor usage | |
| `request_usage.actual_cost_micro_usd` | ✅ | in×8+out×30 | |
| `request_usage.input_tokens_details` | ✅ | vendor usage.input_tokens_details | text_tokens / image_tokens 分拆 |
| `provider_created_at` | ✅ | vendor created | |
