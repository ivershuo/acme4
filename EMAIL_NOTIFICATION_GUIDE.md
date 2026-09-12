# ACME4 邮件通知配置指南

本指南将详细介绍如何在 ACME4 中配置和使用邮件通知功能。

## 概述

ACME4 支持通过 [Resend](https://resend.com/) 服务发送邮件通知，可以在以下情况下自动发送邮件：

- ✅ 证书续期成功
- ❌ 证书续期失败  
- ⚠️ 证书即将到期（续签窗口前及实际到期前 7、3、1 天、过期状态，按阈值持久化去重）

邮件请求总超时为 15 秒。同类持续失败默认每 24 小时最多提醒一次；错误类别变化立即提醒。未确认发送成功的事件不会标记完成，会在后续任务中重试。

## 前置要求

1. 拥有一个域名（用于发件邮箱）
2. Resend 账户和 API Key
3. 已验证的发件域名

## 步骤1: 注册 Resend 账户

1. 访问 [https://resend.com](https://resend.com)
2. 点击 "Sign Up" 注册账户
3. 验证你的邮箱地址
4. 登录到 Resend 控制台

## 步骤2: 验证发件域名

### 2.1 添加域名
1. 在 Resend 控制台中，点击 "Domains"
2. 点击 "Add Domain" 
3. 输入你的域名（例如：`yourdomain.com`）
4. 点击 "Add"

### 2.2 配置 DNS 记录
Resend 会在控制台为当前域名给出需要添加的记录。请逐项复制控制台显示的名称、类型和值，不要照抄其他域名或旧文档中的固定记录；具体记录可能随区域和产品配置变化。

### 2.3 验证域名状态
- 添加 DNS 记录后，等待几分钟到几小时
- 在 Resend 控制台检查域名验证状态
- 状态变为 "Verified" 后即可使用

## 步骤3: 创建 API Key

1. 在 Resend 控制台中，点击 "API Keys"
2. 点击 "Create API Key"
3. 输入描述性名称（例如：`ACME4-Production`）
4. 选择权限（建议选择 "Sending access"）
5. 点击 "Add"
6. **重要**: 立即复制并保存 API Key，它只会显示一次

## 步骤4: 配置 ACME4

### 4.1 编辑配置文件

在你的 `config.yaml` 文件中添加 `email_notification` 部分：

```yaml
email: "your@email.com"
domains:
  - names: ["example.com", "*.example.com"]
    provider: "cloudflare"
    credentials:
      api_token: "your_cloudflare_token"

cert_dir: "/var/lib/acme4/certs"
account_dir: "/var/lib/acme4/accounts"
renew_before: 30

post_renew_hooks: [] # 示例默认不执行生产部署动作

# 邮件通知配置
email_notification:
  enabled: true
  resend_api_key: "re_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"  # 你的 Resend API Key
  from_email: "acme4@yourdomain.com"                     # 发件邮箱
  from_name: "ACME4 Certificate Manager"                 # 发件人名称
  to_emails:
    - "admin@yourdomain.com"                             # 收件人
    - "ops@yourdomain.com"
  notify_on_success: true                                # 成功通知
  notify_on_failure: true                                # 失败通知
  notify_on_expiry: true                                 # 到期提醒
```

### 4.2 配置参数说明

| 参数 | 必填 | 说明 |
|------|------|------|
| `enabled` | 是 | 是否启用邮件通知 |
| `resend_api_key` | 是 | Resend API Key，通常以 `re_` 开头；程序只校验非空，真实性由 Resend 在发送时验证 |
| `from_email` | 是 | 发件邮箱，必须使用已验证域名 |
| `from_name` | 否 | 发件人显示名称 |
| `to_emails` | 是 | 收件人邮箱列表（数组） |
| `notify_on_success` | 否 | 成功时是否发送通知，未配置时默认 true，显式设置 false 可关闭 |
| `notify_on_failure` | 否 | 失败时是否发送通知，未配置时默认 true，显式设置 false 可关闭 |
| `notify_on_expiry` | 否 | 即将到期时是否发送通知，未配置时默认 true，显式设置 false 可关闭 |

主配置文件不会展开 `${ENV_VAR}`。`resend_api_key` 必须出现在最终 YAML 中，因此应将生产配置置于版本库之外并设置为 `0600`，或由配置管理/密钥管理系统在启动前生成该文件。provider 的 DNS 凭据可以使用 `credentials_file`，但该字段目前不适用于 `email_notification`。

## 步骤5: 验证邮件功能

### 5.1 验证配置与单元测试

```bash
# 只读校验严格 YAML 和邮件配置，不发送邮件、不访问 DNS/CA
./acme4 -check-config -config=config.yaml

# 验证邮件模板、开关和 15 秒请求超时等自动测试
go test ./notification
```

当前主程序没有“只发送一封测试邮件”的独立子命令。不要使用旧的 `go run test_email.go` 示例；该文件不是受支持的可执行入口。

### 5.2 验证真实投递

```bash
./acme4 -config=config.yaml
```

真实投递测试会执行正常证书处理流程。应使用独立 staging CA、`account_dir`、`cert_dir`，禁用所有生产 hook，并使用专门的测试收件人。观察日志确认邮件服务启用：

```
邮件通知服务已启用，收件人: [admin@yourdomain.com ops@yourdomain.com]
```

## 邮件模板示例

### 成功通知邮件
```
主题: ✅ 证书续期成功 - example.com, *.example.com

内容包含:
- 域名列表
- 续期时间  
- 新证书到期时间
- 证书有效期（按远近展示为天、小时或分钟）
- 后续命令执行摘要
```

### 失败通知邮件
```
主题: ❌ 证书续期失败 - example.com, *.example.com

内容包含:
- 域名列表
- 失败时间
- 详细错误信息
- 诊断建议（如可识别）
- 通用排查建议
```

### 即将到期提醒邮件
```
主题: ⚠️ 证书即将到期 - example.com, *.example.com

内容包含:
- 域名列表
- 检查时间
- 证书到期时间
- 剩余有效期（按远近展示为天、小时或分钟）
```

## 故障排查

### 常见问题

#### 1. 邮件发送失败 - API Key 错误
```
错误: 发送邮件失败: API key is invalid
解决: 检查 API Key 是否正确，格式应为 re_xxxxxx
```

#### 2. 邮件发送失败 - 域名未验证
```
错误: 发送邮件失败: Domain not verified
解决: 在 Resend 控制台确认域名已完成验证
```

#### 3. 邮件发送失败 - 发件邮箱无效
```
错误: 发送邮件失败: Invalid from email
解决: 确保发件邮箱使用已验证的域名
```

### 日志分析

正常情况下的日志示例：
```
2026-09-12 10:30:15 邮件通知服务已启用，收件人: [admin@example.com]，成功通知: true，失败通知: true，到期提醒: true
2026-09-12 10:30:20 证书 [example.com *.example.com] 已更新并完成部署步骤
2026-09-12 10:30:22 邮件发送成功，ID: 550e8400-e29b-41d4-a716-446655440000，收件人: admin@example.com
```

错误情况下的日志示例：
```
2026-09-12 10:30:15 邮件通知服务已启用，收件人: [admin@example.com]，成功通知: true，失败通知: true，到期提醒: true
2026-09-12 10:30:20 [警告] 邮件通知发送失败: API key is invalid
```

## 安全建议

### 1. API Key 管理
- **不要**将 API Key 提交到版本控制系统
- 定期轮换 API Key
- 将包含密钥的最终配置置于版本库之外，并限制文件权限

### 2. 权限控制
- 为 ACME4 创建专用的 API Key
- 只授予必要的发送权限
- 监控 API 使用情况

### 3. 配置文件保护
```bash
# 设置配置文件权限
chmod 600 config.yaml

# 确保 .gitignore 包含配置文件
echo "config.yaml" >> .gitignore
```

## 高级配置

### 密钥注入边界

ACME4 当前不解析 shell 风格的环境变量占位符。若部署平台以环境变量或密钥存储提供 Resend 凭据，应在启动 ACME4 之前用受控模板流程生成权限为 `0600` 的最终 YAML；不要把 `${RESEND_API_KEY}` 原样写入配置并期待程序展开，也不要把生成后的文件提交到版本控制。

### 多环境配置

为不同环境创建不同的配置文件：

```bash
# 开发环境
config.dev.yaml

# 测试环境  
config.test.yaml

# 生产环境
config.prod.yaml
```

使用时指定配置文件：
```bash
./acme4 -config=config.prod.yaml
```

## 费用说明

Resend 的免费额度、每日限制和可验证域名数量可能调整。部署前请查看 [Resend 当前价格页](https://resend.com/pricing)，不要依赖本文保存的固定额度。

## 通知语义与限制

- 续签成功事件按证书版本去重；投递未确认成功时会在后续运行重试。
- 临期提醒覆盖进入续签窗口前 7、3、1 天，以及实际到期前 7、3、1 天和已过期状态；一次漏跑跨过多个阈值时只补最高紧迫级别。
- 同一失败类别默认 24 小时最多通知一次；失败类别变化会立即通知。通知状态保存在对应证书的 `cert_dir/.acme4/` 状态目录。
- 邮件发送失败只记录警告，不会把已经成功的签发或部署改判为失败。
- 配置 YAML 无法解析、邮件配置本身无效或邮件服务初始化前发生的错误无法通过该配置发送邮件；必须依靠进程退出码和日志监控兜底。
- API 请求总超时为 15 秒。发送接口响应丢失时无法保证严格“恰好一次”，收件人可能收到重复邮件。

## 技术支持

如果遇到问题：

1. 查看 ACME4 日志输出
2. 检查 Resend 控制台的发送日志
3. 验证 DNS 配置和域名状态
4. 参考 [Resend 官方文档](https://resend.com/docs)

---

*本指南与当前仓库实现同步；升级依赖或通知逻辑后应同时复核本文。*
