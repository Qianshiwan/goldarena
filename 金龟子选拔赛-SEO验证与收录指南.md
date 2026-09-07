# 金龟子选拔赛 · SEO 验证与收录指南

> 站点：https://www.kinguizi.top （后端已动态生成 robots.txt / sitemap.xml，并包含 /about.html）

## 已完成（代码层）
- [x] `web/public/about.html` 服务端渲染落地页（爬虫可直接读正文）
- [x] `web/index.html` 加 `<noscript>` 正文 + JSON-LD 结构化数据
- [x] 后端 `main.go` 动态生成 `robots.txt`（已排除 /api /admin）与 `sitemap.xml`（含 10 个 URL，含 /about.html）
- [x] `web/public/goldarena.svg` 修复 favicon/og 坏链
- [x] `web/public/verify/` 目录：放置搜索引擎验证文件（百度/Bing 下发文件原样放此，部署后公开可访问）

## 待你手动完成（需登录搜索引擎后台）
### 1. 提交 sitemap
- 百度搜索资源平台：https://ziyuan.baidu.com → 站点管理 → 提交 → `https://www.kinguizi.top/sitemap.xml`
- Bing Webmaster Tools：https://www.bing.com/webmasters → 添加站点 → 提交 `https://www.kinguizi.top/sitemap.xml`

### 2. 站点验证（二选一）
**方式 A：文件验证（推荐，已就绪）**
1. 在百度/Bing 后台选"文件验证"，下载/复制验证文件（如 `baidu_verify_XXXX.html` 或 `BingSiteAuth.xml`）。
2. 把文件（保留原始名与内容）发给管理员 / AI，由它写入 `web/public/verify/<原文件名>`，重新构建部署。
3. 平台访问 `https://www.kinguizi.top/<原文件名>` 即可验证通过。

**方式 B：meta 标签验证**
1. 平台给一段 `<meta name="baidu-site-verification" content="code-XXXX" />`（或 Bing 类似标签）。
2. 发给管理员 / AI，加进 `web/index.html` 的 `<head>`，重新构建部署。

### 3. 主动引蜘蛛（加速收录）
- 在贴吧、知乎、交易论坛、雪球等发带主站链接的介绍帖（见《外链推广文案》）。
- 提交这些外链页面 URL 到百度/Bing 的"普通收录 / URL 提交"。
- 条件允许可换 1–2 个高质量友链。

## 预期时间线
提交 sitemap + 验证后，通常 3–14 天可被搜到；搜"金龟子选拔赛""金归子 黄金"等品牌词可见。通用词"金龟子"竞争大，建议主推"金龟子选拔赛""金归子"等长尾品牌词。

## 验证命令（部署后自测）
```bash
curl -s https://www.kinguizi.top/robots.txt
curl -s https://www.kinguizi.top/sitemap.xml | grep -o '<loc>[^<]*</loc>'
curl -s https://www.kinguizi.top/about.html | grep -o '<title>[^<]*</title>'
```
