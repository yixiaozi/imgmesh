# imgmesh

官网：https://imgmesh.com

旅行照片的加密链接分享。发送者在 iOS 上选照片，手机本地加密后上传；对方用浏览器打开链接，或在 App 里存进相册。密钥只放在链接的 `#k=` 片段里。

约定写在 [docs/architecture.md](docs/architecture.md)。`testdata/vector.json` 是 iOS、网页和参考实现共用的加密向量。

## 目录

- `index.html`、`styles.css`、`favicon.svg`：营销站
- `web/`：浏览器查看器，构建后放在 `/s/`
- `server/`：Go API
- `ios/`：SwiftUI 工程，用 Xcode 打开 `ios/Imgmesh.xcodeproj`
- `desktop/`：Windows 和 Mac 共用的 Electron 客户端。在对应系统上执行 `cd desktop && npm ci && npm run dist`。推送 `v*` 标签后，GitHub Actions 会生成这两个系统的安装包并挂到该版本
- `deploy/`：systemd、Nginx 片段和服务器安装脚本

## iOS

用 Xcode 16 打开工程，把签名团队选成你的 Apple Developer 团队，Bundle ID 保持 `com.imgmesh.app`。目标系统是 iOS 17。

App ID 需要打开 Sign in with Apple 和 Associated Domains。服务器上的 `/etc/imgmesh/api.env` 把 `APPLE_TEAM_ID` 写成团队 ID 后重启 `imgmesh-api`，通用链接才会从 `https://imgmesh.com/s/…` 直接打开 App。没配对之前，把完整链接粘贴进 App 也能打开并保存。

每台设备先登记一个不重复的名称。Apple 登录只用于发出和撤销。收到链接的人也要登记，查看、下载和保存才会记在这个名称下。照片网格里可以打开每张照片的身份证；资料里的介绍、城市和联系方式可以用来找到对方。

## 服务器

API 监听 `127.0.0.1:8097`，由 imgmesh.com 的 Nginx 反向代理。数据库是本机的 `imgmesh-postgres`，密文在 `/var/lib/imgmesh/blobs`。推送到 `main` 会构建 API 和查看器并执行 `deploy/remote-setup.sh`。
