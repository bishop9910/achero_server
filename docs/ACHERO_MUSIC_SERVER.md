# Achero 音乐服务器 — 服务端实现与协议

本文描述本仓库（Kratos 驱动的 Go 音乐服务器）如何实现 Achero 内置插件
`com.achero.musicServer` 的 RPC 协议，并给出部署与验证方法。

> 机器可读的 OpenAPI 3.1 描述见仓库根目录 `openapi.yaml`（`make openapi` 校验）。

---

## 1. 传输层

- **端点**：一个固定的 HTTP URL，默认 `http://<host>:8080/rpc`（可用
  `music.rpc_path` 修改）。
- **方法**：`POST`。
- **请求头**：
  - `Content-Type: application/json`
  - （可选）`Authorization: Bearer <token>`
- **请求体**：标准 JSON-RPC 2.0 请求对象。

```json
{ "jsonrpc": "2.0", "id": 1, "method": "music.list", "params": { "offset": 0, "limit": 200 } }
```

- **响应体**：标准 JSON-RPC 2.0 响应（成功 `result`，失败 `error`）。

```json
{ "jsonrpc": "2.0", "id": 1, "result": { "tracks": [ /* ... */ ] } }
```

## 2. 鉴权

| 用途 | 方式 | 说明 |
| --- | --- | --- |
| RPC 元数据调用 | `Authorization: Bearer <token>` 请求头 | `music.token` 非空时强制 |
| 音频流 / 封面 | **签名 URL**（`?token=<签名>`） | 见下方签名格式 |

### 签名 URL

当 `music.stream_secret`（或回退的 `music.token`）非空时，`music.list` /
`music.streamUrl` 返回的 `url`、`coverUrl` 会携带签名令牌：

```
http://host:8080/stream/<id>?token=<expires>:<hex>
```

- `expires`：Unix 秒时间戳，`now + music.token_ttl`。
- `hex`：`HMAC-SHA256(secret, "<purpose>:<id>:<expires>")` 的十六进制摘要。
- `purpose`：`stream` 或 `cover`，用于绑定令牌用途，防止跨端点重放。
- 令牌过期或篡改时，服务端返回 `403 Forbidden`。

> 两个密钥都为空时，URL 不带签名，流与封面完全开放。

## 3. 方法

### 3.1 `music.ping`

参数为空，返回 `{"ok": true}`。

```json
{ "jsonrpc": "2.0", "id": 0, "method": "music.ping", "params": {} }
→ { "jsonrpc": "2.0", "id": 0, "result": { "ok": true } }
```

### 3.2 `music.list`

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `offset` | int | 否 | 分页偏移，默认 0 |
| `limit` | int | 否 | 每页数量，默认 200 |

响应 `result` 包含 `tracks`（数组）与 `total`（曲目总数，便于分页）。

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "total": 1,
    "tracks": [
      {
        "id": "3a2c1f9e8b7d6c5a4b3a2c1f",
        "title": "海阔天空",
        "artist": "Beyond",
        "album": "乐与怒",
        "durationMs": 324000,
        "url": "http://192.168.1.10:8080/stream/3a2c1f9e8b7d6c5a4b3a2c1f?token=...",
        "coverUrl": "http://192.168.1.10:8080/cover/3a2c1f9e8b7d6c5a4b3a2c1f?token=...",
        "lyrics": "[00:00.00]今天我\n[00:05.00]寒夜里看雪飘过"
      }
    ]
  }
}
```

### 3.3 `music.listAlbums`（可选）

按专辑浏览。客户端不强制要求实现，失败会自动回退到纯歌曲列表。

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `offset` | int | 否 | 分页偏移，默认 0 |
| `limit` | int | 否 | 每页数量，默认 200 |

响应 `result` 含 `albums`（数组）与 `total`：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "total": 1,
    "albums": [
      { "id": "a1", "name": "乐与怒", "artist": "Beyond", "coverUrl": "...", "songCount": 10, "year": 1993 }
    ]
  }
}
```

### 3.4 `music.listArtists`（可选）

按艺术家浏览。客户端不强制要求实现。

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `offset` | int | 否 | 分页偏移，默认 0 |
| `limit` | int | 否 | 每页数量，默认 200 |

响应 `result` 含 `artists`（数组）与 `total`：

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "total": 1,
    "artists": [
      { "id": "ar1", "name": "Beyond", "albumCount": 8, "songCount": 96, "coverUrl": "..." }
    ]
  }
}
```

### 3.5 `music.listSongs`（可选）

按专辑或艺术家拉取曲目。`albumId` / `artistId` 至少提供其一；同时提供时以服务器
实现为准（本实现会同时满足两者，即取交集）。

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `albumId` | string | 二选一 | 按专辑拉取曲目 |
| `artistId` | string | 二选一 | 按艺术家拉取曲目 |
| `offset` | int | 否 | 分页偏移，默认 0 |
| `limit` | int | 否 | 每页数量，默认 200 |

响应曲目结构与 [`music.list`](#32-musiclist) 一致：

```json
{ "jsonrpc": "2.0", "id": 1, "result": { "tracks": [ /* ... */ ], "total": 1 } }
```

### 3.6 `music.streamUrl`

当 `music.list` 未返回 `url` 时按曲目 id 解析流地址（本实现始终在 list 中返回
`url`，此方法作为兜底）。

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 曲目 id |

```json
{ "jsonrpc": "2.0", "id": 2, "method": "music.streamUrl", "params": { "id": "..." } }
→ { "jsonrpc": "2.0", "id": 2, "result": { "url": "http://.../stream/...?token=..." } }
```

## 4. 数据结构

### Track

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 曲目唯一 id（相对路径的 SHA-256 前缀，不透明、防目录穿越） |
| `title` | string | 是 | 标题（标签缺失时回退为文件名） |
| `artist` | string | 否 | 艺术家 |
| `album` | string | 否 | 专辑 |
| `durationMs` | int | 否 | 时长（毫秒），解析失败时为 0 |
| `url` | string | 否 | 流地址（可带签名） |
| `coverUrl` | string | 否 | 封面地址（仅内嵌封面存在时返回） |
| `lyrics` | string | 否 | 内联 LRC 文本（存在同名 `.lrc` 时返回） |

### Album

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 专辑唯一 id（`sha256(专辑艺术家 + "\0" + 专辑名)` 前缀） |
| `name` | string | 是 | 专辑名 |
| `artist` | string | 否 | 专辑艺术家（`AlbumArtist` 标签，缺省回退曲目艺术家） |
| `coverUrl` | string | 否 | 封面地址（指向代表曲目的封面） |
| `songCount` | int | 否 | 曲目数 |
| `year` | int | 否 | 发行年份（取自标签，缺失时不返回） |

### Artist

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 艺术家唯一 id（`sha256(艺术家名)` 前缀） |
| `name` | string | 是 | 艺术家名 |
| `albumCount` | int | 否 | 专辑数 |
| `songCount` | int | 否 | 曲目数 |
| `coverUrl` | string | 否 | 头像/封面地址（指向代表曲目的封面） |

## 5. 错误处理

失败返回 JSON-RPC 错误对象；传输层错误（鉴权失败、方法不允许）返回对应 HTTP
状态码。

| 场景 | code | HTTP |
| --- | --- | --- |
| 请求体不是合法 JSON | `-32700` | 200 |
| 请求不合法（缺 `method` / `jsonrpc`） | `-32600` | 200 |
| 未知方法 | `-32601` | 200 |
| 参数缺失 / 非法 | `-32602` | 200 |
| 曲目不存在 | `-32001` | 200 |
| 内部错误 | `-32000` | 200 |
| 未授权（Bearer 缺失 / 错误） | — | 401 |
| 流令牌无效 / 过期 | — | 403 |
| 请求方法不允许 | — | 405 |

## 6. 曲目来源与元数据

服务端启动时递归扫描 `music.music_dir`：

- **索引**：按配置的扩展名收集音频文件，`id = sha256(相对路径)[:24]`，稳定且不泄露
  文件系统结构。
- **标签**：用 `github.com/dhowden/tag` 读取 `title` / `artist` / `album` /
  `album_artist` / `year` 与内嵌封面（ID3v2、MP4、FLAC、Vorbis）。
- **专辑/艺术家**：扫描后按标签聚合出 `Album` / `Artist` 视图（供 `listAlbums` /
  `listArtists` / `listSongs` 使用）——专辑按「专辑艺术家 + 专辑名」归并，艺术家按
  曲目艺术家归并；缺标签时回退为「未知专辑」/「未知艺术家」。专辑/艺术家的封面
  取自组内第一首带内嵌封面的曲目。
- **时长**：内置解析器按格式计算——
  - MP3：Xing/Info VBR 帧数，否则 CBR 估算；
  - FLAC：STREAMINFO 总采样数 / 采样率；
  - M4A/M4B：`moov/mvhd` 的 timescale 与 duration；
  - Ogg/Opus：末页 granule position；
  - WAV：`fmt` byte rate 与 `data` 大小。
  解析失败时 `durationMs` 为 0（协议中为可选字段）。
- **歌词**：同名 `.lrc` 文件（如 `song.mp3` → `song.lrc`）会内联到 `lyrics`；
  读取时自动把 GBK/GB2312 编码归一化为 UTF-8。
- **JSON 侧车（自定义元数据 / 封面）**：音乐文件旁的**同名 JSON**（`song.mp3` →
  `song.json`）可覆盖元数据，**优先级高于音乐文件自身标签**。支持字段：`title` /
  `artist` / `album` / `albumArtist` / `year` / `lyrics` / `cover`。其中 `cover`
  与 `lyrics` 都**只允许相对路径**（相对 JSON 所在目录），且解析后必须仍在
  `music_dir` 内——绝对路径与 `..` 越界都会被拒绝：
  - `cover`：指向封面图片文件；
  - `lyrics`：以 `.lrc` / `.txt` 结尾时视为指向歌词文件（同样做 GBK 归一化），
    否则视为内联 LRC 文本。
  JSON 里未填写的字段回退到音乐文件标签。

  ```json
  { "title": "海阔天空", "artist": "Beyond", "year": 1993, "cover": "cover.jpg", "lyrics": "lyrics/foo.lrc" }
  ```

- **热重载**：`music.watch: true`（默认）时用 fsnotify 监听目录，文件新增/删除/
  改名会触发**防抖后的自动重扫**（默认 2 秒内无新事件才重建索引），无需重启；
  同时自动为新建的子目录补挂监听。`music.rescan_interval` 作为轮询兜底（对
  不支持监听的文件系统/网络挂载更可靠）。两者可同时开启。
- **索引原子替换**：重扫会整体重建并原子换入新索引，读取端无阻塞、无需加锁等待。

> 首次扫描需逐个读取标签，超大曲库可能耗时数秒到数十秒；日志会输出
> `music library indexed count=…`，热重载时也会输出同样的行确认已刷新。

## 7. 音频流

`GET /stream/{id}?token=…`（以及 `HEAD`）返回音频字节流：

- 由 `http.ServeContent` 处理，**完整支持 `Range` 请求**（`206 Partial Content`、
  `Accept-Ranges: bytes`），保证拖动进度条可用。
- `Content-Type` 取自文件扩展名（如 `audio/mpeg`、`audio/flac`）。
- 若 `music.token` / `music.stream_secret` 已配置，`token` 缺失或无效返回 `403`。

封面端点 `GET /cover/{id}?token=…` 返回内嵌封面（`Cache-Control: public`，缓存 1 天）。

## 8. 配置参考

见 `configs/config.yaml`，全部项：

| 键 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `server.http.addr` | string | `0.0.0.0:8080` | 监听地址 |
| `server.http.timeout` | duration | `0s` | 处理器超时；**保持 0**，避免中断流式播放 |
| `music.music_dir` | string | `${MUSIC_DIR:./music}` | 扫描目录 |
| `music.base_url` | string | `""` | 公网基址 `scheme://host[:port]`；空则按请求推导 |
| `music.token` | string | `""` | RPC Bearer 令牌；空 = 不鉴权 |
| `music.stream_secret` | string | `""` | 签名密钥；空 = 回退 `token` |
| `music.token_ttl` | int64 | `3600` | 签名 URL 有效期（秒） |
| `music.rpc_path` | string | `/rpc` | JSON-RPC 路径 |
| `music.stream_path` | string | `/stream` | 流路径前缀 |
| `music.cover_path` | string | `/cover` | 封面路径前缀 |
| `music.extensions` | []string | 内置列表 | 索引扩展名 |
| `music.rescan_interval` | duration | `0s` | 轮询重扫间隔，0 关闭 |
| `music.watch` | bool | `true` | 用 fsnotify 监听目录，文件增删/改名时自动重扫（热重载） |

环境变量：配置支持 `${VAR:default}` 语法，且 `KRATOS_` 前缀的环境变量可覆盖
（如 `KRATOS_MUSIC_TOKEN`）。

## 9. 部署

### 本地

```bash
# 直接运行（Windows 下编译产物为 bin/acheron_server.exe）
go run ./cmd/acheron_server -conf ./configs

# 或编译后运行
go build -o ./bin/ ./...
./bin/acheron_server -conf ./configs
```

### Docker

```bash
docker build -t acheron-music-server .
docker run --rm -p 8080:8080 \
  -v "$PWD/configs:/data/conf" \
  -v "$PWD/music:/music" \
  -e MUSIC_DIR=/music \
  acheron-music-server
```

### 验证（curl）

```bash
# 健康检查
curl -s -X POST http://localhost:8080/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":0,"method":"music.ping","params":{}}'

# 拉取列表
curl -s -X POST http://localhost:8080/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"music.list","params":{"offset":0,"limit":10}}'

# 拉取专辑 / 艺术家 / 按专辑拉歌（可选方法）
curl -s -X POST http://localhost:8080/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"music.listAlbums","params":{}}'
curl -s -X POST http://localhost:8080/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"music.listArtists","params":{}}'
curl -s -X POST http://localhost:8080/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"music.listSongs","params":{"albumId":"<album-id>"}}'

# 带鉴权（配置 music.token 后）
curl -s -X POST http://localhost:8080/rpc \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <token>' \
  -d '{"jsonrpc":"2.0","id":1,"method":"music.list","params":{}}'
```

## 10. 参考服务端要点

本实现即是一个可直接使用的参考服务端。若需要自研，最小要求：

```
POST /rpc             → 解析 JSON-RPC，分发 music.ping / music.list / music.streamUrl
GET  /stream/{id}?token=… → 返回音频字节流（务必支持 Range）
```

若要支持按专辑 / 艺术家浏览，再实现三个可选方法：`music.listAlbums`、
`music.listArtists`、`music.listSongs`。

> 实现服务端时务必支持 **HTTP Range 请求**，否则进度条拖动与流式播放体验会受影响。
