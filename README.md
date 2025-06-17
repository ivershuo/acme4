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
```

### 2. 运行

```sh
go build -o acme4
./acme4 -config=config.yaml
```

### 3. crontab 自动化（示例）
```sh
0 3 * * * /path/to/acme4 -config=/path/to/config.yaml >> /var/log/acme4.log 2>&1
```

## 目录结构说明
- `main.go`        主程序入口
- `providers/`     所有 DNS Provider 插件（可扩展）
- `config.sample.yaml` 配置示例
- `.gitignore`     忽略敏感和临时文件
- `README.md`      项目说明

## 扩展 Provider
在 `providers/` 目录下添加新的 provider 文件，并在 `providers/providers.go` 注册即可。

## 证书更新后自动操作
- 在 `config.yaml` 的 `post_renew_hooks` 字段配置 shell 命令，如 `nginx -s reload`。
- 每次证书更新后会自动依次执行这些命令，并将输出写入日志。

## 注意事项
- 请勿将包含真实密钥的 `config.yaml`、`certs/`、`accounts/` 目录提交到公开仓库。
- 仅将 `config.sample.yaml` 用作模板。
