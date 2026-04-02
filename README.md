# acme4

自动化 ACME 证书申请、续期与钩子集成工具

## 功能简介
- 支持多域名、多 DNS Provider（Cloudflare、Hurricane、TencentCloud、Porkbun 等）
- 证书自动申请与续期，支持泛域名
- 证书更新后自动执行自定义命令（如 nginx reload 等）
- 配置结构清晰，易于扩展

## 快速开始

### 1. 配置文件
请参考 `config.sample.yaml`，并复制为 `config.yaml`，填写实际邮箱、API Key、证书目录、钩子命令等：

```yaml
email: "your@email.com"
domains:
  - names: ["example.com", "*.example.com"]
    provider: "hurricane"
    credentials:
      api_key: "example.com=HE_API_KEY"
  - names: ["example.net"]
    provider: "cloudflare"
    credentials:
      api_token: "CF_xxx"
  # ...更多域名
cert_dir: "./certs"
account_dir: "./accounts"
post_renew_hooks:
  - "nginx -s reload"
renew_before: 30   # 可选，证书到期前多少天自动续期，默认30天

# 邮件通知配置（可选）
email_notification:
  enabled: true
  resend_api_key: "re_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  from_email: "acme4@yourdomain.com"
  from_name: "ACME4 Certificate Manager"
  to_emails:
    - "admin@yourdomain.com"
    - "ops@yourdomain.com"
  notify_on_success: true
  notify_on_failure: true
  notify_on_expiry: true
```

Hurricane Electric 的 `credentials.api_key` 不是单个 token，而是兼容 `HURRICANE_TOKENS` 的 `key=value[,key=value...]` 映射串：

```yaml
domains:
  - names: ["example.com", "*.example.com"]
    provider: "hurricane"
    credentials:
      api_key: "example.com=HE_API_KEY"
```

如需给特定记录覆盖 token，可以使用完整主机名作为 key：

```yaml
domains:
  - names: ["example.com", "*.example.com"]
    provider: "hurricane"
    credentials:
      api_key: "example.com=HE_API_KEY,_acme-challenge.example.com=HE_RECORD_TOKEN"
```

### 2. 运行

```sh
go build -o acme4
./acme4 -config=config.yaml
```

#### 检查远程主机证书信息

可通过 `-ssl-domain` 参数快速检测远程主机的 TLS 证书信息：

```sh
./acme4 -ssl-domain=example.com
```

### 3. crontab 自动化（示例）
```sh
0 3 * * * /path/to/acme4 -config=/path/to/config.yaml >> /var/log/acme4.log 2>&1
```

## 目录结构说明
- `main.go`        主程序入口
- `providers/`     所有 DNS Provider 插件（可扩展）
- `config.sample.yaml` 配置示例
- `README.md`      项目说明

证书和私钥文件命名规则：
- 以第一个域名为文件名，存放于 `cert_dir` 目录下，如 `example.com.crt`、`example.com.key`

## 扩展 Provider
在 `providers/` 目录下添加新的 provider 文件，并在 `providers/providers.go` 注册即可。例如：

```go
func init() {
    RegisterProvider("yourprovider", newYourProvider)
}
```

Provider 工厂函数签名：
```go
func(domain Domain) (challenge.Provider, error)
```

## 证书更新后自动操作
- 在 `config.yaml` 的 `post_renew_hooks` 字段配置 shell 命令，如 `nginx -s reload`。
- 每次证书更新后会自动依次执行这些命令，并将输出写入日志。
- 日志中会详细记录证书处理、钩子执行的成功与失败，并给出排查建议。

## 邮件通知功能
- 支持通过 [Resend](https://resend.com/) 服务发送邮件通知
- 在证书续期成功、失败或即将到期时自动发送邮件
- 需要先在 Resend 注册账户并获取 API Key
- 需要验证发件邮箱的域名

### 配置邮件通知
1. 在 [Resend](https://resend.com/) 注册账户
2. 创建 API Key
3. 验证你的发件域名
4. 在 `config.yaml` 中添加 `email_notification` 配置项：

```yaml
email_notification:
  enabled: true                                    # 启用邮件通知
  resend_api_key: "re_xxxxxxxxxxxxxxxxxxxxxxxx"   # Resend API Key
  from_email: "acme4@yourdomain.com"              # 发件邮箱（必须已验证域名）
  from_name: "ACME4 Certificate Manager"          # 发件人名称
  to_emails:                                      # 收件人列表
    - "admin@yourdomain.com"
    - "ops@yourdomain.com"
  notify_on_success: true                         # 续期成功通知
  notify_on_failure: true                         # 续期失败通知
  notify_on_expiry: true                          # 即将到期通知
```

## 错误处理与日志
- 所有关键步骤均有详细日志输出，便于排查问题。
- 若遇到配置或权限等致命错误，程序会终止并给出详细提示。
- 钩子命令执行失败时会输出错误和命令返回内容。
- 邮件通知发送失败时会在日志中记录警告信息，不会影响证书续期流程。

## 注意事项
- 请勿将包含真实密钥的 `config.yaml`、`certs/`、`accounts/` 目录提交到公开仓库。
- 仅将 `config.sample.yaml` 用作模板。
- 使用邮件通知功能时，请妥善保管 Resend API Key，不要提交到版本控制系统。
- 邮件通知功能完全可选，禁用后不会影响证书续期的正常运行。
