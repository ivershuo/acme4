# Tencent Upload Cert Hook

这个 hook 会把当前续期成功的证书上传到腾讯云 SSL 证书服务，并输出证书 ID。

## 编译

在仓库根目录执行：

```sh
go build -o hooks/tencent-upload-cert/tencent-upload-cert ./hooks/tencent-upload-cert
```

## 使用方式

推荐使用结构化 `hooks` 显式传入当前不可变版本的证书、私钥和别名，并通过 group/other 无权限（例如 `0600` 或 `0400`）的 `env_file` 提供凭据：

```yaml
hooks:
  - id: upload-tencent
    command: ./hooks/tencent-upload-cert/tencent-upload-cert
    args: ["--cert", "{cert_path}", "--key", "{key_path}", "--alias", "{domain}"]
    timeout: 30s
    env_file: /etc/acme4/tencent-upload.env
```

`env_file` 不执行 shell，也不展开变量；它不是完整 dotenv 解析器，只接受简化的 `KEY=value` 行。不要写 `export`、行尾注释或包裹值的引号，否则它们会被当成键值内容。文件内容应为：

```text
TENCENTCLOUD_SECRET_ID=AKID...
TENCENTCLOUD_SECRET_KEY=...
```

相对 `env_file` 路径按 acme4 进程工作目录解析。也可以在直接运行上传工具时预先设置这两个环境变量；不要把 `SecretKey` 写入 hook 命令。

旧 `post_renew_hooks` 字符串模式仍兼容，但会经过 shell，没有结构化 hook 的超时、输出限制和脱敏保证，并可能在日志中显示展开后的命令。

占位符由 `acme4` 主程序替换：

- `{domain}`: 当前证书的主域名
- `{cert_path}`: 当前证书文件路径
- `{key_path}`: 当前私钥文件路径

## 命令行参数

支持两种输入模式：

```sh
./hooks/tencent-upload-cert/tencent-upload-cert \
  --cert ./certs/example.com.crt \
  --key ./certs/example.com.key \
  --alias example.com
```

上面的参数组合是推荐用法：`--cert`、`--key` 和 `--alias` 一起使用，
不要再同时传入 `--domain`。凭据从环境变量读取。

为兼容按域名推导文件路径的旧配置，也可以使用：

```sh
./hooks/tencent-upload-cert/tencent-upload-cert \
  --domain example.com \
  --cert-dir ./certs
```

可选参数：

- `--alias`：上传到腾讯云时使用的证书别名

如果没有指定 `--alias`，程序会默认使用 `--domain`，或者使用 `--cert` 的完整文件名（包含扩展名）作为别名。生产配置建议显式传入 `--alias`。

## 凭证

推荐通过环境变量传入：

- `TENCENTCLOUD_SECRET_ID`
- `TENCENTCLOUD_SECRET_KEY`

`--secret-id` 和 `--secret-key` 仅为已有调用和临时调试保留，生产部署不推荐使用，
因为命令行参数可能被同机用户通过进程信息和 shell history 看到。命令行参数优先于同名环境变量。

## 输出

成功时会输出类似：

```text
uploaded certificate_id=cert-123 request_id=req-1
```

上传请求使用 `Repeatable=false`。首次上传返回 `CertificateId`；如果腾讯云
判定证书已存在，则返回 `RepeatCertId`，hook 会把它作为同一个成功的证书 ID
返回，不会因为重复执行而产生新的证书。

请求超时为 30 秒。超时、腾讯云 API 错误、证书或私钥不匹配都会返回非 0
exit code；上传成功仅表示证书已进入腾讯云 SSL 证书服务，不表示它已经绑定到
CDN、CLB 或其他线上资源。

失败时返回非 0 exit code，并把错误输出到 stderr。
