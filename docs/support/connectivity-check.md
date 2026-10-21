# 单独运行网络检测

`backend/cmd/connectivity-check` 构建为单独的 Windows 程序，复用桌面端 `desktopnet.Proxy`，分别检查 BeefTV 当前线路与不使用 HTTP 代理的线路。存储检测只发送无凭据 HEAD，API 检测发送无凭据 GET 并最多读取 4 KiB 响应；不读取应用数据、密钥或客户素材，代理地址只用于本机连接、不写入报告，不提交生成请求，不修改系统代理。

向用户发送完整压缩包，用户解压后双击 `BeefTV-network-check.exe`，保持与报错时相同的代理设置，完成后回传同目录的 TXT 和 JSON 报告。系统可能提示未签名程序；该工具并非正式安装包或应用更新。

`targets.json` 最多包含四个 HTTPS 目标，支持 `/`、`/v1/videos`、`/api/status`；禁止用户名、密码、查询参数、片段及非 443 端口。支持人员应填写客户实际使用的 API 域名和当前实际 COS 桶域名，不能用通用 COS 官网代替故障地址。

```json
{"targets":[{"name":"BeefTV API","url":"https://beeftv.app/v1/videos"},{"name":"广州 COS","url":"https://YOUR_BUCKET.cos.ap-guangzhou.myqcloud.com/"}]}
```

每个目标、每种线路各测两次；TCP 最多 10 秒、TLS 最多 15 秒、单次总时限 25 秒。报告记录实际是否使用代理、失败阶段、DNS/TCP/TLS/首字节/总耗时和 HTTP 状态，不记录解析出的 IP 或代理地址。

私有 COS 的 403 和未登录 API 的 401 表示已收到 HTTPS 响应，不代表鉴权、素材上传或模型生成成功。工具不探测完整文件上传；服务端须另用自有合成素材核对存储链路。“不使用 HTTP 代理”不能绕过系统 VPN/TUN。PAC/WPAD 不由工具执行；报告会提示仅有自动代理配置的情况，这与当前 BeefTV 的处理方式相同。

构建及验证：

```sh
cd backend
go test -race ./cmd/connectivity-check ./internal/desktopnet
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o BeefTV-network-check.exe ./cmd/connectivity-check
```

命令行支持 `-config targets.json -out . -pause=false`，便于在测试主机上保存可核对的报告。工具不会自动发送报告或上传任何检测结果。
