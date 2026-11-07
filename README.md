# netmusic — 50MB 级的网易云音乐桌面播放器

一个**手写 Win32 + 纯 Go** 的网易云音乐桌面客户端，外观参考千千静听的主控窗口（暗色窄条 + 独立播放列表/歌词/评论窗口）。

设计上只有一个优先级：**常驻内存**。所以这里没有 WebView、没有 Qt/GTK、没有 FFmpeg、没有 cgo。

```
────────────────────────────────────────────────
 ■ netmusic                灰(未登录) ▣ ─ ✕
 ┌────────┐  卡农（经典钢琴版）
 │  封面  │  dylanf
 │        │  卡农 Canon in D
 └────────┘  ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁  01:12 / 05:00
             ⏮  ⏸  ⏭   顺序  🔊 ▁▁▁   列表 歌词 评论
────────────────────────────────────────────────
```

## 一、硬指标达成情况

| 指标 | 要求 | 实测 | 结论 |
|---|---|---|---|
| 常驻内存 RSS（全功能） | < 100 MB | **39.4 MB** | ✅ 目标 50MB 以内 |
| 评论模块增量 | < 20 MB（否则降级） | **+1.26 MB** | ✅ 未触发降级 |
| 单进程 GUI | 必须 | ✅ 一个进程 5 个窗口 | ✅ |
| 原生控件/轻量绑定 | 禁止 WebView | ✅ user32/gdi32/comctl32 直调 | ✅ |
| 系统音频 API | 优先 WASAPI | ✅ WASAPI 共享模式（手写 COM） | ✅ |
| 无大型多媒体框架 | 禁止 FFmpeg 全量 | ✅ 仅纯 Go MP3 解码（约 200KB 代码） | ✅ |

同机对照：官方网易云音乐客户端（cloudmusic.exe）当时占用 **333 MB**，本项目 **39 MB**。

## 二、内存实测记录

测量方式：进程内调用 `GetProcessMemoryInfo`（PSAPI）+ `runtime.ReadMemStats`，每阶段采样一次；
同时用外部 `Get-Process` 复核（两者一致，见文末「外部复核」）。**不是估算**。

### 2.1 分阶段实测（`netmusic.exe -selftest -stage 5`）

| 阶段 | 场景 | RSS (WS) | 私有字节 | Go Heap | Go Sys | GDI | USER |
|---|---|---|---|---|---|---|---|
| stage0 | 主控窗口 + 登录窗口（二维码已渲染） | 22.87 MB | 51.46 MB | 2.31 MB | 11.02 MB | 32 | 20 |
| stage1 | + 播放列表窗口 + 搜索 8 首结果 | 24.05 MB | 52.64 MB | 2.54 MB | 11.27 MB | 36 | 23 |
| stage2 | + 正在播放（音频管线 + 封面 + 歌词已加载） | 37.32 MB | 62.48 MB | 9.30 MB | 20.17 MB | 38 | 25 |
| stage3 | + 歌词窗口 | 38.01 MB | 63.07 MB | 6.63 MB | 20.17 MB | 40 | 26 |
| stage4 | **+ 评论窗口（热门 15 条）** | 39.27 MB | 63.90 MB | 7.39 MB | 20.17 MB | 42 | 27 |
| stage5 | **+ 评论窗口（最新第 2 页）** | 39.19 MB | 63.77 MB | 7.40 MB | 20.17 MB | 42 | 26 |

### 2.2 评论模块开启前 / 后 对比（要求的核心数据）

| | 评论前 (stage3) | 评论后 (stage4) | 增量 |
|---|---|---|---|
| RSS | 38.01 MB | 39.27 MB | **+1.26 MB** |
| Go Heap | 6.63 MB | 7.39 MB | +0.76 MB |
| 私有字节 | 63.07 MB | 63.90 MB | +0.83 MB |
| GDI 对象 | 40 | 42 | +2 |

翻到第 2 页（stage5）后 RSS 反而降了 0.08MB（GC 抖动），不再增长 ⇒ **翻页不累积内存**。

### 2.3 长跑稳定性（`netmusic.exe -soak 10`，10 次切歌，全部窗口开启）

```
soak 0 : WS=38.09MB  Heap= 8.04MB  GDI=40  USER=25
soak 1 : WS=37.51MB  Heap= 6.24MB  GDI=40  USER=23
soak 2 : WS=40.82MB  Heap= 9.70MB  GDI=42  USER=23
soak 3 : WS=39.45MB  Heap= 7.48MB  GDI=42  USER=23
soak 4 : WS=34.62MB  Heap= 3.90MB  GDI=42  USER=24
soak 5 : WS=40.28MB  Heap= 9.92MB  GDI=42  USER=24
soak 6 : WS=36.57MB  Heap= 5.30MB  GDI=42  USER=24
soak 7 : WS=35.50MB  Heap= 4.10MB  GDI=42  USER=25
soak 8 : WS=45.31MB  Heap=10.80MB  GDI=42  USER=25
soak 9 : WS=43.70MB  Heap= 5.12MB  GDI=42  USER=25
soak10 : WS=36.85MB  Heap= 4.69MB  GDI=42  USER=25
```

- RSS 在 **34.6 – 45.3 MB** 之间随 GC 波动，**没有上升趋势**（起点 38.09，终点 36.85）。
- GDI 对象恒为 40–42，USER 对象 23–25 ⇒ **无句柄泄漏**。
- Heap 峰值 10.8MB，被 `debug.SetMemoryLimit(64MB)` 和按需分配压在低位。

### 2.4 外部复核

```
Get-Process netmusic | WS=39.38MB  Private=63.95MB  Threads=13  Handles=441
```

> 说明：任务管理器「内存」列显示的是**专用工作集**，会比这里的 Private 略低；
> Process Explorer 的 `Private Bytes` 对应上表「私有字节」列。RSS/WS 是我们承诺的那个数。

## 三、功能清单

### P0（全部实现）

- ✅ **二维码登录**：登录窗口实时渲染二维码（210×210），2 秒轮询扫码状态（801 等待 / 802 已扫码 / 803 成功 / 800 过期），成功后自动关闭并同步歌单
- ✅ **手机号 + 密码登录**：weapi 加密（AES-128-CBC ×2 + 无填充 RSA，全部标准库实现）
- ✅ **账号本地持久化**：Cookie / UID / 昵称写入 `%APPDATA%\netmusic\config.json`，下次启动免登录
- ✅ **我喜欢的音乐 + 用户歌单**：左侧歌单列表，`我喜欢的音乐` 置顶
- ✅ **点击播放**：双击歌曲播放；上一首 / 下一首 / 暂停 / 继续；点击进度条任意位置 seek
- ✅ **播放信息**：标题、歌手、专辑、封面（98×98，切歌异步加载并释放旧图）
- ✅ **歌词同步滚动**：当前行高亮居中，支持翻译行合并，点击任意行跳转，滚轮浏览后 4 秒回到跟随
- ✅ **评论**：热门 / 最新两个标签页，最新按 20 条/页分页（上一页/下一页 + 页码 + 总数），异步加载

### P1（已实现）

- ✅ 播放队列管理：加入队列 / 上移 / 下移 / 移除（播放列表窗口底部按钮）
- ✅ 音量调节：滑块拖动，就地缩放采样（`(v/100)²` 曲线）
- ✅ 播放模式：顺序 / 单曲循环 / 随机（点击「顺序」循环切换，持久化）

### 明确不做（按需求）

社交动态、直播、商城、MV、评论发帖/点赞/回复（只读）、皮肤系统、均衡器与音效开关。

## 四、构建

### 环境要求

- **Windows 10 / 11 x64**（Go 1.21+ 运行时的限制，见「已知限制」）
- Go 1.21+（本项目用 Go 1.27.3 验证）
- 无需 gcc、无需 MSVC、无需 Free Pascal：`CGO_ENABLED=0`

### 编译

```powershell
git clone <this-repo> netmusic
cd netmusic

# 依赖只有两个纯 Go 包：go-mp3（解码）与 go-qrcode（登录二维码）
go mod download

# 发布构建（静态单文件，约 7.4 MB）
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" -o netmusic.exe .\cmd\netmusic
```

国内网络建议先设好模块代理：

```powershell
go env -w GOPROXY=https://goproxy.cn,direct
```

### 可选构建

```powershell
# 32 位（内存更省，适合 2GB 老机器；需 32 位 Windows）
$env:GOOS="windows"; $env:GOARCH="386"; $env:CGO_ENABLED="0"
go build -trimpath -ldflags "-s -w" -o netmusic32.exe .\cmd\netmusic
```

## 五、运行

```powershell
.\netmusic.exe
```

首次启动会自动弹出登录窗口：手机扫码，或切到「手机号登录」输入账号密码。
登录后点主控窗口的 **列表** 打开歌单，双击歌曲播放。

### 快捷键 / 交互

| 操作 | 效果 |
|---|---|
| 空格 | 播放 / 暂停（主控窗口聚焦时） |
| ← / → | 上一首 / 下一首 |
| 拖动标题条 | 移动窗口（吸附由 Windows 原生处理） |
| 点击进度条 / 时间 | seek 到对应位置 |
| 点击「顺序 / 单曲 / 随机」 | 切换播放模式 |
| 点击封面 | 打开播放列表 |
| 标题条右侧「灰(未登录)」 | 打开登录窗口 |
| 歌词窗口点击某行 | 跳到该句 |
| 评论窗口「热门 / 最新 / 刷新 / 上一页 / 下一页」 | 评论导航 |

### 命令行参数（自检与内存复现）

```powershell
.\netmusic.exe -selftest -stage 5   # 免登录播放一首免版权歌曲并逐阶段记录内存
.\netmusic.exe -soak 10             # 连续切歌 10 次，每轮记录内存与 GDI 句柄
.\netmusic.exe -no-gc-limit         # 关闭 64MB 软堆上限（用于对比测量）
```

## 六、内存是怎么省下来的

| 手段 | 效果 |
|---|---|
| 不用 WebView / Qt / GTK | 省掉 60–150MB |
| 手写 Win32 + 自绘，只缓存画刷/画笔/字体 | 空窗口整进程 12.2MB |
| 不使用 GDI+ / WIC，图片用 `image/jpeg` 解码后直接进 32bpp DIB | 不加载系统图像框架 |
| 音频只驻留**压缩后的整曲**（1–3MB），按需解码到设备缓冲 | 比"全曲预解码成 PCM"省 50MB/首 |
| WASAPI `AUTOCONVERTPCM` 交给系统重采样 | 不写重采样代码、无额外缓冲 |
| JSON 直接从 `resp.Body` 流式解码，不缓存原始响应 | 歌单 1MB JSON 不产生 1MB 常驻 |
| 评论分页 + 自绘虚拟列表 | 评论内存与总条数无关，190 万条≈20 条 |
| `debug.SetMemoryLimit(64MB)` | GC 更早介入，堆峰值压在 11MB |
| 后台任务结果用单条 `PostMessage` 回 UI 线程 | 无线程轮询、无锁竞争、无批量拷贝 |

细节见 [`docs/01-feasibility.md`](docs/01-feasibility.md) 与 [`docs/02-architecture.md`](docs/02-architecture.md)。

## 七、已知限制与降级说明

**没有因内存被砍掉的功能**。评论模块实测 +1.26MB，远低于 20MB 的降级阈值，因此热门 + 最新 + 分页全部保留。

需要如实说明的几点：

1. **Windows 10+ only**。Go 1.21 起运行时不再支持 Windows 7/8。若目标机是 Win7，需要 Go 1.20 编译（业务代码基本兼容，需去掉 `debug.SetMemoryLimit`、把 `atomic.Int32/Bool` 换成 `atomic.LoadInt32` 形式），本项目未做该适配。
2. **登录成功路径未由我端到端验证**：扫码登录的二维码生成与轮询状态机（801/802/803/800）已实测跑通，但"确认登录"需要真人拿手机扫码，我无法代做。手机号登录的 weapi 加密链路已实现但同样未用真实账号验证。若登录接口返回异常，用 `nmprobe` 可以直接定位是加密、Cookie 还是风控问题。
3. **未登录时只能播放免版权曲目**（`fee=0` / `fee=8`）。VIP/版权曲目需要在登录态下才有直链，这是服务端限制。
4. **音质固定 128kbps**。`config.json` 里 `quality` 可改 320000（RSS 约 +2MB），但界面上还没有音质选择入口。
5. 评论富文本（表情/图片/@）按需求**降级为纯文本**，`[呲牙]` 这类表情名原样显示，不做图片化。
6. 播放队列是内存态，不跨进程持久化（歌单本身在服务端，不受影响）。
7. 只解码 MP3。若接口返回 FLAC，代码会自动改用 320kbps MP3 直链；纯 FLAC 播放不支持（这是为内存做的取舍）。

## 八、目录结构

```
netmusic/
├─ cmd/netmusic/        程序入口（DPIAware、GC 上限、消息循环）
├─ cmd/nmprobe/         调试 CLI：search / url / play / bench / lyric / comments / qr / playlists / audio
├─ internal/
│  ├─ w32/              手写 Win32 绑定：core.go（GDI+DIB）、win.go（窗口/消息/控件）、
│  │                    mem.go（PSAPI 采样）、reg.go 内联于 netx
│  ├─ ui/               theme.go（配色/字体/绘图原语）、mainwindow.go（主控窗口）、
│  │                    panel.go（窗口基类）、widgets.go（虚拟列表）、
│  │                    playlistwindow.go、lyricswindow.go、commentswindow.go、
│  │                    loginwindow.go、edit.go（原生输入框 + Enter 子类化）
│  ├─ app/              编排层：状态机、队列、异步加载、登录流程
│  ├─ audio/            wasapi.go（COM 接口）、player.go（解码 + 控制循环）
│  ├─ netease/          client.go（API）、crypto.go（weapi/eapi）、md5.go
│  ├─ netx/             代理（直连/系统 WinINET/自定义）与 HTTP 客户端
│  ├─ lrc/              LRC 解析与二分查找
│  └─ config/           JSON 配置持久化
├─ docs/                可行性评估、架构设计
├─ .tools/              截图/测量辅助脚本（开发期使用）
└─ go.mod               运行时依赖：go-mp3、go-qrcode
```

## 九、关于参考项目（go-musicfox）

只参考了**后端思路**，没有引入它的任何代码或终端 UI 层：

- `weapi` 加密流程（`AES-CBC(presetKey) → b64 → AES-CBC(随机密钥) → b64` + 无填充 RSA 得 `encSecKey`）——用标准库重写约 90 行，并修掉了原实现 RSA 输出丢失前导零导致 `encSecKey` 长度不定的问题。
- 端点与参数形状（`/api/song/enhance/player/url`、`/api/song/lyric`、`/api/v1/resource/comments/R_SO_4_*`、`/api/v6/playlist/detail`、`/api/nuser/account/get`、`/api/login/qrcode/*`）。
- 本项目优先选择**无需加密的 `/api/` 接口**，加密只用于手机号登录一条路径，依赖面最小。

为什么不破坏内存约束：go-musicfox 的内存大头是终端 UI（tcell/bubbletea）和音频库（beep/gopxl），本项目一个都没用；唯一保留的运行时依赖是纯 Go、无 cgo、约 200KB 代码的 go-mp3。

## 十、调试工具

`nmprobe` 是不带界面的同款后端，用于在无 GUI 环境下定位问题：

```powershell
.\nmprobe.exe search 周杰伦            # 搜索
.\nmprobe.exe url 1384554             # 取直链
.\nmprobe.exe bench 1384554           # 整曲解码计时 + seek 计时
.\nmprobe.exe audio 1384554           # 真实走 WASAPI 播放 5 秒（含 seek）
.\nmprobe.exe lyric 186016            # 歌词
.\nmprobe.exe comments 186016 20      # 评论（第 2 页）
.\nmprobe.exe qr                      # 二维码登录状态机
.\nmprobe.exe proxy                   # 分辨率代理配置
```

## 十一、许可

本项目仅用于学习与技术验证。音乐数据版权归网易云音乐及各版权方所有，请勿用于商业用途。
