# imgmesh

官网：https://imgmesh.com

旅行照片加密链接分享（先做 iOS）。本仓库目前托管营销首页静态站。

## 本地文件

- `index.html` / `styles.css` / `favicon.svg` — 站点内容
- `deploy/nginx-imgmesh.conf` — Nginx 参考配置

## 自动发布

推送到 `main`，或在 GitHub 上 **Publish release**，会触发 GitHub Actions，把站点同步到服务器 `/var/www/imgmesh/`。

也可在 Actions 里手动运行 **Deploy Website**。
