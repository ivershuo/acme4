# ACME4 邮件通知配置指南

本指南将详细介绍如何在 ACME4 中配置和使用邮件通知功能。

## 概述

ACME4 支持通过 [Resend](https://resend.com/) 服务发送邮件通知，可以在以下情况下自动发送邮件：

- ✅ 证书续期成功
- ❌ 证书续期失败  
- ⚠️ 证书即将到期（距离续期还有7天时）

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
Resend 会提供需要添加的 DNS 记录，通常包括：

```
类型: TXT
名称: @
值: resend-verification=xxxxxxxx

类型: MX  
名称: @
值: feedback-smtp.resend.com
优先级: 10

类型: TXT
名称: @
值: v=spf1 include:_spf.resend.com ~all

类型: TXT
名称: resend._domainkey
值: p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA...
```

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

cert_dir: "./certs"
account_dir: "./accounts"
renew_before: 30

post_renew_hooks:
  - "nginx -s reload"

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
| `resend_api_key` | 是 | Resend API Key，格式为 `re_xxxxxx` |
| `from_email` | 是 | 发件邮箱，必须使用已验证域名 |
| `from_name` | 否 | 发件人显示名称 |
| `to_emails` | 是 | 收件人邮箱列表（数组） |
| `notify_on_success` | 否 | 成功时是否发送通知，默认 true |
| `notify_on_failure` | 否 | 失败时是否发送通知，默认 true |
| `notify_on_expiry` | 否 | 即将到期时是否发送通知，默认 true |

## 步骤5: 测试邮件功能

### 5.1 使用测试脚本

```bash
# 测试成功通知
go run test_email.go \
  -api-key="re_your_api_key_here" \
  -from="acme4@yourdomain.com" \
  -to="admin@yourdomain.com" \
  -type="success"

# 测试失败通知  
go run test_email.go \
  -api-key="re_your_api_key_here" \
  -from="acme4@yourdomain.com" \
  -to="admin@yourdomain.com" \
  -type="failure"

# 测试到期提醒
go run test_email.go \
  -api-key="re_your_api_key_here" \
  -from="acme4@yourdomain.com" \
  -to="admin@yourdomain.com" \
  -type="expiry"
```

### 5.2 运行 ACME4 测试

```bash
./acme4 -config=config.yaml
```

观察日志输出，确认邮件服务启用：

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
- 证书有效期
```

### 失败通知邮件
```
主题: ❌ 证书续期失败 - example.com, *.example.com

内容包含:
- 域名列表
- 失败时间
- 详细错误信息
- 排查建议
```

### 即将到期提醒邮件
```
主题: ⚠️ 证书即将到期 - example.com, *.example.com

内容包含:
- 域名列表
- 检查时间
- 证书到期时间
- 剩余有效期
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
2024-01-15 10:30:15 邮件通知服务已启用，收件人: [admin@example.com]
2024-01-15 10:30:20 证书 [example.com *.example.com] 已更新
2024-01-15 10:30:22 邮件发送成功，ID: 550e8400-e29b-41d4-a716-446655440000，收件人: admin@example.com
```

错误情况下的日志示例：
```
2024-01-15 10:30:15 邮件通知服务已启用，收件人: [admin@example.com]  
2024-01-15 10:30:20 证书 [example.com *.example.com] 已更新
2024-01-15 10:30:22 [警告] 邮件通知发送失败: API key is invalid
```

## 安全建议

### 1. API Key 管理
- **不要**将 API Key 提交到版本控制系统
- 定期轮换 API Key
- 使用环境变量存储敏感信息

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

### 环境变量支持

可以通过环境变量设置敏感信息：

```bash
export RESEND_API_KEY="re_your_api_key_here"
export ACME4_FROM_EMAIL="acme4@yourdomain.com"
```

然后在配置文件中引用：
```yaml
email_notification:
  enabled: true
  resend_api_key: "${RESEND_API_KEY}"
  from_email: "${ACME4_FROM_EMAIL}"
  # ... 其他配置
```

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

Resend 提供免费额度：
- 每月 3,000 封免费邮件
- 100 个已验证域名
- 标准支持

对于 ACME4 的使用场景（证书续期通知），免费额度通常足够使用。

## 技术支持

如果遇到问题：

1. 查看 ACME4 日志输出
2. 检查 Resend 控制台的发送日志
3. 验证 DNS 配置和域名状态
4. 参考 [Resend 官方文档](https://resend.com/docs)

---

*本指南适用于 ACME4 v1.0+，如有更新请参考最新文档。*