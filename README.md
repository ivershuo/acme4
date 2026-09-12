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

只检查严格 YAML、域名、provider 凭据、输出冲突及 resolver 等配置，不创建目录、不访问 DNS/CA、也不执行 hook：

```sh
./acme4 -check-config -config=config.yaml
```

每个 provider 可以用 `credentials_file` 替代内联 `credentials`。文件是严格 YAML 键值映射、相对配置文件目录解析，必须是仅所有者可读写的普通文件（例如 `0600`），且不能同时配置两种来源。

推荐使用结构化 `hooks`：

```yaml
hooks:
  - id: reload-nginx
    command: /usr/sbin/nginx
    args: ["-s", "reload"]
    timeout: 120s
  - id: upload-tencent
    command: /opt/acme4/tencent-upload-cert
    args: ["--cert", "{cert_path}", "--key", "{key_path}", "--alias", "{domain}"]
    timeout: 30s
    env_file: /etc/acme4/tencent-upload.env
```

结构化 hook 参数不会经过 shell 二次解释，默认超时 120 秒，输出最多保留 64 KiB。`env_file` 按 `KEY=VALUE` 读取而不执行 shell，且不会继承 DNS、ACME 或邮件密钥。旧 `post_renew_hooks` 保留原 shell 语义，两种格式不可同时使用；旧模式可能把展开后的命令写入日志，因此不要把凭据放入命令参数。

新证书先保存在 `cert_dir/.acme4/` 的不可变版本目录并校验完整性，再原子更新 `<首域名>.crt/.key` 兼容路径，让 reload 类 hook 能读取新版本。hook 失败状态会持久化，上一完整版本仍保留用于恢复；下一轮只重试未完成或配置变化后的 hook，不重新申请证书。外部命令在进程中断边界只能保证至少执行一次，因此 hook 应具备幂等性。

#### 检查远程主机证书信息

可通过 `-ssl-domain` 参数快速检测远程主机的 TLS 证书信息：

```sh
./acme4 -ssl-domain=example.com
# 连接 IP/端口，但使用指定虚拟主机 SNI 和主机名校验：
./acme4 -ssl-domain=192.0.2.10:443 -ssl-server-name=example.com
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
- 优先使用前文的结构化 `hooks`；`post_renew_hooks` 仅用于兼容需要 shell 语义的旧配置。
- 两种 hook 都支持 `{domain}`、`{cert_path}`、`{key_path}`，其中证书路径指向同一个不可变版本。
- 部署状态按证书版本和 hook 标识持久化；成功步骤不会在失败重试时重复执行。
- 结构化 hook 日志只显示标识、结果和脱敏后的受限输出。

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

`notify_on_success`、`notify_on_failure`、`notify_on_expiry` 未配置时默认启用，显式设置为 `false` 可关闭对应通知。提醒覆盖进入续签窗口前 7、3、1 天以及实际到期前 7、3、1 天和过期状态；按跨越阈值触发并持久化去重。持续同类失败默认每天最多通知一次，错误类别变化会立即通知；发送未确认成功的事件保留到后续运行重试。

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
