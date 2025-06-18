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
      api_key: "HE_API_KEY"
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

## 错误处理与日志
- 所有关键步骤均有详细日志输出，便于排查问题。
- 若遇到配置或权限等致命错误，程序会终止并给出详细提示。
- 钩子命令执行失败时会输出错误和命令返回内容。

## 注意事项
- 请勿将包含真实密钥的 `config.yaml`、`certs/`、`accounts/` 目录提交到公开仓库。
- 仅将 `config.sample.yaml` 用作模板。
