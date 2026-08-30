# Achero Music Server

一个 **Kratos 驱动**、纯 Go 实现的「Achero 音乐服务器」服务端。它实现
[Achero 音乐服务器 RPC 协议](docs/ACHERO_MUSIC_SERVER.md)（JSON-RPC 2.0 over
HTTP），从一个本地音乐目录扫描曲目元数据，并对外提供**流式播放**接口。

- 零原生依赖，跨平台（Windows / Linux / macOS）单二进制。
- 无数据库：曲目索引在启动时扫描目录并驻留内存。
- 可选鉴权：RPC 用 `Authorization: Bearer <token>`，音频流用 **HMAC 签名 URL**。
- 支持 HTTP **Range** 请求（拖动进度条）、内嵌封面与 `.lrc` 歌词（GBK/GB2312 自动转 UTF-8）。
- 支持**歌曲 / 专辑 / 艺术家**三种浏览：`music.list` + `music.listAlbums` /
  `music.listArtists` / `music.listSongs`。
- 支持**同名 JSON 自定义元数据/封面/歌词文件**（`song.mp3` → `song.json`，优先级高于
  文件标签，封面与歌词文件路径只允许相对路径），见[协议文档 §6](docs/ACHERO_MUSIC_SERVER.md#6-曲目来源与元数据)。
- **热重载**：fsnotify 监听目录，新增/删除/改名音乐文件自动刷新曲库，无需重启。

## 快速开始

```bash
# 1. 准备音乐目录（mp3/flac/m4a/ogg/opus/wav）
mkdir -p ./music

# 2. 直接运行（Windows 下编译产物为 bin/achero_server.exe）
go run ./cmd/achero_server -conf ./configs

# 或编译后运行
go build -o ./bin/ ./...
./bin/achero_server -conf ./configs
```

默认监听 `http://0.0.0.0:8080`。在 Achero 的「音乐服务器」插件中填入：

- 服务器 RPC 地址：`http://<host>:8080/rpc`
- 访问令牌：留空（除非在配置里设置了 `music.token`）

然后点「连接并获取列表」即可。

## 项目结构

```text
cmd/achero_server/    入口、Wire 依赖注入
configs/               运行配置（config.yaml）
internal/conf/         配置 proto（make config 生成）
internal/server/       HTTP server 组装、CORS 中间件
internal/service/      JSON-RPC / stream / cover 传输适配层
internal/biz/          Track 领域模型、MusicUsecase、repo 接口
internal/data/         文件系统扫描/索引/热重载实现
internal/data/audio/   元数据提取 + 各格式时长解析
tools/openapi/         OpenAPI 文档结构校验器（make openapi）
docs/                  协议与部署文档
openapi.yaml           OpenAPI 3.1 描述文件
```

分层遵循仓库的 `AGENTS.md` 约定：`service` 只做 DTO↔DO 转换与传输适配，
`biz` 持有领域模型与 repo 接口，`data` 实现文件系统仓储。

## 配置

见 [configs/config.yaml](configs/config.yaml)。核心项：

| 键 | 说明 | 默认 |
| --- | --- | --- |
| `music.music_dir` | 递归扫描的音乐目录 | `${MUSIC_DIR:./music}` |
| `music.token` | RPC 端点的 Bearer 令牌；空 = 不鉴权 | `""` |
| `music.stream_secret` | 流/封面 URL 的 HMAC 密钥；空 = 回退到 `token` | `""` |
| `music.base_url` | 构造 URL 用的公网基址；空 = 从请求 Host 推导 | `""` |
| `music.token_ttl` | 签名 URL 有效期（秒） | `3600` |
| `music.extensions` | 索引的音频扩展名 | mp3/flac/m4a/m4b/ogg/opus/wav |
| `music.rescan_interval` | 定期重扫间隔；`0s` = 关闭 | `0s` |
| `music.watch` | 用 fsnotify 监听目录，文件增删/改名时自动重扫（热重载） | `true` |

## 鉴权

- **关闭**（默认）：`token` 与 `stream_secret` 都为空，RPC 与流均开放。
- **仅 RPC 鉴权**：设置 `token`，RPC 要求 `Authorization: Bearer <token>`。
- **RPC + 流鉴权**：设置 `token`（并可选覆盖 `stream_secret`），流/封面 URL 会
  带上 HMAC 签名令牌，见[协议文档 §2](docs/ACHERO_MUSIC_SERVER.md#2-鉴权)。

## 开发命令

```bash
make config     # 重新生成 internal/conf 的 protobuf
make openapi    # 校验 openapi.yaml（结构校验 + 打印预览提示）
make generate   # 重新生成 Wire 注入 + go mod tidy
make build      # 编译
go test ./...   # 测试
```

## API 文档

协议是 JSON-RPC（非 protobuf），因此没有 `make api` → protoc-gen-openapi 的生成
链路。API 文档有两处、同一内容：

- [`docs/ACHERO_MUSIC_SERVER.md`](docs/ACHERO_MUSIC_SERVER.md) — 权威 Markdown 文档。
- [`openapi.yaml`](openapi.yaml) — OpenAPI 3.1 描述（`POST /rpc`、`GET /stream/{id}`、
  `GET /cover/{id}`），可粘贴到 [Swagger Editor](https://editor.swagger.io/) 或
  `npx @redocly/cli preview-docs openapi.yaml` 预览；`make openapi` 做结构校验。

## Docker

```bash
docker build -t acheron-music-server .
docker run --rm -p 8080:8080 \
  -v "$PWD/configs:/data/conf" \
  -v "$PWD/music:/music" \
  acheron-music-server
```

## 协议与部署

- 完整 RPC 协议：见 [docs/ACHERO_MUSIC_SERVER.md](docs/ACHERO_MUSIC_SERVER.md)。
- 客户端使用方式见 Achero 内置插件「音乐服务器」。
