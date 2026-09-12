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
    # _acme-challenge.example.com 在 Hurricane 中 CNAME 到 Cloudflare
    # 的专用验证域名；names 不需要改成委派后的域名。
    provider: "cloudflare"
    credentials:
      api_token: "CF_xxx"
  - names: ["example.net"]
    provider: "hurricane"
    credentials:
      api_key: "example.net:HE_API_KEY"
  # ...更多域名
cert_dir: "./certs"
account_dir: "./accounts"
post_renew_hooks:
  - "nginx -s reload"
renew_before: 30   # 可选，证书到期前多少天自动续期，默认30天
# 可选：Let's Encrypt staging；生产配置可省略并使用默认 CA。
# acme_directory_url: "https://acme-staging-v02.api.letsencrypt.org/directory"

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

Hurricane Electric 的 `credentials.api_key` 不是单个 token，而是兼容 `HURRICANE_TOKENS` 的
`key:token[,key:token...]` 映射串。冒号是 lego 当前要求的分隔符；旧配置中的 `=` 仅作为迁移兼容格式，建议改为冒号：

```yaml
domains:
  - names: ["example.com", "*.example.com"]
    provider: "hurricane"
    credentials:
      api_key: "example.com:HE_API_KEY"
```

如需给特定记录覆盖 token，可以使用完整主机名作为 key：

```yaml
domains:
  - names: ["example.com", "*.example.com"]
    provider: "hurricane"
    credentials:
      api_key: "example.com:HE_API_KEY,_acme-challenge.example.com:HE_RECORD_TOKEN"
```

凭据匹配时，完整记录主机名（例如 `_acme-challenge.example.com`）优先于域名级 token（`example.com`）。因此，只有确实为某条记录单独生成 token 时才添加完整主机名项；不要依赖映射项顺序覆盖 token。

Hurricane Electric 的动态 DNS TXT 更新更适合顺序验证。程序会对 Hurricane provider 使用更保守的默认值：

- `HURRICANE_PROPAGATION_TIMEOUT`: 默认 `300` 秒。
- `HURRICANE_SEQUENCE_INTERVAL`: 默认 `120` 秒，用于同一证书内多个 DNS-01 challenge 之间的等待。
- `HURRICANE_INTERVAL_RETRIES`: 默认 `3` 次，只在 Hurricane 返回 `interval` 限流时重试。
- `HURRICANE_INTERVAL_RETRY_WAIT`: 默认 `30` 秒，限流重试初始等待时间，后续按 2 倍退避。

如果 `example.com` 和 `*.example.com` 共享 `_acme-challenge.example.com`，程序会打印风险提示，并按 lego 的顺序验证流程逐个写入、验证和清理 TXT 记录。

### Hurricane 业务 DNS + Cloudflare 验证委派

推荐把业务 DNS 继续放在 Hurricane，只把 ACME 验证记录委派给专用的 Cloudflare zone。以 `example.com` 为例：

```text
Hurricane（业务 zone）
_acme-challenge.example.com  CNAME  example-com.acme.validation-domain.tld.

Cloudflare（专用验证 zone）
example-com.acme.validation-domain.tld  TXT  <由 ACME 本次挑战写入的值>
```

在 Hurricane 中预先创建 CNAME，并等待旧 TXT 的 TTL/缓存过期；Cloudflare 中的目标 TXT 由 lego 创建和按记录 ID 清理，不要手工固定挑战值。每个不同的原始 challenge 主机名使用独立目标；根域名和对应泛域名共享同一 `_acme-challenge.example.com` CNAME。不同原始域名（例如 `example.net`）应使用另一个目标（如 `example-net.acme.validation-domain.tld`）。

委派后，配置中的 `names`、证书路径和 hook 保持不变，只把该条目的 `provider` 改为 `cloudflare`，因为 Cloudflare 是实际写入验证 TXT 的 provider。Cloudflare API token 只授予专用验证 zone 的 DNS 编辑和 Zone 读取权限，不要使用账户级全局 API Key。生产迁移前应在独立 staging 配置中验证 CNAME 跟随、同名多 TXT 共存和精确清理。

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
# 每 6 小时检查一次；定时与手动执行必须使用同一 account_dir 才能互斥
0 */6 * * * cd /path/to/acme4 && /path/to/acme4 -config=/path/to/config.yaml >> /var/log/acme4.log 2>&1
```

程序默认在证书到期前 30 天进入续签窗口；有效证书会在每轮检查中跳过，失败则留到下一轮重试。单机任务会对 `account_dir` 加锁，锁被占用时以非零状态退出。

### staging 验证

staging 必须使用独立的配置、账户目录、证书目录和 hook 设置，避免测试账户、证书或部署动作污染生产：

```yaml
acme_directory_url: "https://acme-staging-v02.api.letsencrypt.org/directory"
cert_dir: "./certs-staging"
account_dir: "./accounts-staging"
post_renew_hooks: []
```

重复测试应使用新的 staging 账户或清理 staging 状态，确保实际触发 DNS-01 challenge，不要把 CA 授权缓存命中当作 DNS 委派验收。迁移到生产时恢复生产 CA、生产目录和经审阅的 hook 配置。

## 目录结构说明
- `main.go`        主程序入口
- `hooks/`         内置续期后 hook 命令
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
- hook 命令支持占位符：`{domain}`、`{cert_path}`、`{key_path}`。
- 每次证书更新后会自动依次执行这些命令，并将输出写入日志。
- 日志中会详细记录证书处理、钩子执行的成功与失败，并给出排查建议。

## 邮件通知功能
- 支持通过 [Resend](https://resend.com/) 服务发送邮件通知
- 在证书续期成功、失败或即将进入续期窗口时自动发送邮件
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

`notify_on_success`、`notify_on_failure`、`notify_on_expiry` 未配置时默认启用，显式设置为 `false` 可关闭对应通知。到期提醒会在进入续期窗口前 7、3、1 天发送，避免每天重复提醒；邮件中的剩余有效期会按远近展示为天、小时或分钟。

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
