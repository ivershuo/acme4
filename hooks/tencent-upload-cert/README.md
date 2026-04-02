# Tencent Upload Cert Hook

这个 hook 会把当前续期成功的证书上传到腾讯云 SSL 证书服务，并输出新的 `CertificateId`。

## 编译

在仓库根目录执行：

```sh
go build -o hooks/tencent-upload-cert/tencent-upload-cert ./hooks/tencent-upload-cert
```

## 使用方式

推荐在 `post_renew_hooks` 里显式传入当前证书路径和域名：

```yaml
post_renew_hooks:
  - "./hooks/tencent-upload-cert/tencent-upload-cert --cert {cert_path} --key {key_path} --domain {domain} --secret-id ${TENCENTCLOUD_SECRET_ID} --secret-key ${TENCENTCLOUD_SECRET_KEY}"
```

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
  --secret-id "$TENCENTCLOUD_SECRET_ID" \
  --secret-key "$TENCENTCLOUD_SECRET_KEY"
```

或者：

```sh
./hooks/tencent-upload-cert/tencent-upload-cert \
  --domain example.com \
  --cert-dir ./certs \
  --secret-id "$TENCENTCLOUD_SECRET_ID" \
  --secret-key "$TENCENTCLOUD_SECRET_KEY"
```

可选参数：

- `--alias`：上传到腾讯云时使用的证书别名

如果没有指定 `--alias`，程序会默认使用 `--domain`，或者从 `--cert` 文件名推导。

## 凭证

首选通过命令行参数传入：

- `--secret-id`
- `--secret-key`

也支持环境变量 fallback：

- `TENCENTCLOUD_SECRET_ID`
- `TENCENTCLOUD_SECRET_KEY`

## 输出

成功时会输出类似：

```text
uploaded certificate_id=cert-123 request_id=req-1
```

失败时返回非 0 exit code，并把错误输出到 stderr。
