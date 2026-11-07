# 第 2 步：架构设计

## 2.1 模块划分与依赖方向

```
cmd/netmusic        程序入口：DPIAware、GC 上限、创建 App、跑消息循环
cmd/nmprobe        调试用 CLI（搜索/直链/歌词/评论/解码速度/音频自检），不参与发布
        │
   internal/app    编排层：把 UI、API、播放器粘起来；唯一持有全局状态的地方
     │   │   │
     │   │   └────────── internal/audio      播放器：WASAPI + go-mp3 + 控制循环
     │   └────────────── internal/netease    网易云 API 客户端（登录/歌单/直链/歌词/评论/搜索）
     │   └────────────── internal/netx       代理解析 + *http.Client 构造
     ├────────────────── internal/ui         Win32 界面：窗口、自绘控件、虚拟列表、输入框
     ├────────────────── internal/lrc        LRC 解析（时间戳二分查找）
     └────────────────── internal/config     JSON 配置持久化（原子写）
                              │
                        internal/w32      手写 Win32 绑定（syscall，无 cgo）
```

依赖是**单向**的：`w32` 不认识任何人，`ui` 只依赖 `w32` + `lrc`，`app` 认识全部，没有反向依赖。

## 2.2 线程模型（避免拷贝与锁竞争的关键）

| 线程 | 职责 | 约束 |
|---|---|---|
| UI 线程（`runtime.LockOSThread`） | 所有 Win32 调用、所有 UI 状态 | 任何后台线程**不得**触碰窗口或 UI 字段 |
| 音频线程（独立 goroutine + `LockOSThread`+`CoInitializeEx`） | COM/WASAPI 全部调用、MP3 解码 | 独占 `IAudioClient`/`IAudioRenderClient`/`*mp3.Decoder` |
| 网络 goroutine | HTTP、JSON 解码、图片解码 | 结果通过 `post()` 回到 UI 线程 |

**后台 → UI 的唯一通道**：

```go
func (a *App) post(f func()) {
    select {
    case a.uiQ <- f:                                  // 闭包入队
        w32.PostMessage(a.Main.HWND(), WMDispatch, 0, 0)  // 只有 1 个消息
    default:                                          // 队列满则丢弃，绝不阻塞工作线程
    }
}
```

- 只有一个 `PostMessage`（`WM_APP+100`），不轮询、不 sleep 等待 UI。
- 队列满时**丢弃**而不是阻塞：宁可丢一次界面刷新，也不让音频/网络线程卡住。
- UI 线程在 `drain()` 里一次性排空队列，避免消息风暴。

**跨界数据零拷贝策略**

| 数据 | 做法 |
|---|---|
| 歌曲音频 | 下载得 `[]byte` 一次性交给播放器（所有权转移），播放器内 `bytes.Reader` 复用，**不再复制** |
| 解码 → 设备 | `IAudioRenderClient::GetBuffer` 返回的裸指针用 `unsafe.Slice` 包一层直接解码写入，**解码结果不经过任何中间缓冲** |
| 音量 | 就地缩放 int16，不产生新切片 |
| 封面/二维码 | 解码 → 缩放到目标尺寸的 DIB（**一次性**），GDI 直接 `BitBlt` |
| 列表行文本 | 列表只存 `[]Track`（结构体切片），绘制时按索引取，**不预生成字符串** |

## 2.3 界面结构（复刻千千静听的多窗口布局）

| 窗口 | 尺寸 | 实现 |
|---|---|---|
| 主控窗口 | 420×152 | 无边框 `WS_POPUP`，整窗自绘：标题条 + 封面 + 曲目信息 + 进度条 + 走带按钮 + 音量 + 模式 + 三个功能钮 |
| 播放列表 | 470×430 | 左侧歌单列表 + 右侧歌曲列表 + 搜索框（原生 EDIT） |
| 歌词 | 400×330 | 居中滚动歌词，当前行高亮，点击行跳转 |
| 评论 | 470×400 | 热门/最新 + 分页 + 虚拟列表 |
| 登录 | 340×430 | 二维码 / 手机号 + 密码 |

- 无边框窗口的拖动与边框吸附交给 Windows：`WM_NCHITTEST` 在标题条返回 `HTCAPTION`，在按钮区返回 `HTCLIENT`。
- 每个窗口一份**双缓冲 DIB**（`BackBuffer`），仅在窗口变大时重建；稳态绘制零分配。
- GDI 对象（画刷/画笔/字体）全部缓存复用，实测全窗口开启后 **GDI 对象 40 个、USER 对象 25 个**，长跑无增长。

## 2.4 评论模块：分页 + 虚拟化

这是本项目唯一有「数据量不可控」风险的模块，分三层限制：

**① 传输层分页。** `POST /api/v1/resource/comments/R_SO_4_<id>`，`limit=20`、`offset` 递增。
单页响应 51KB，**只为当前页创建 Go 对象**，且 `encoding/json` 直接从 `resp.Body` 解码（`json.NewDecoder`），不缓存响应字节。

**② 模型层裁剪。** 响应里每条评论含完整 user 对象、头像、IP 归属、权限位等；只抽取
`nickname / content / likedCount / time / beReplied[0].user`，其余在解码时即被丢弃。
富文本（`[em]xxx[/em]`、`[img]`、`@`）在 `flatten()` 里降级为纯文本，**不引入富文本渲染引擎**。

**③ 渲染层虚拟化。** `ui.ListView` 是自绘虚拟列表：

```go
func (l *ListView) Paint(dc HDC) {
    first, last := l.visibleRows()   // 只算可视区间
    for i := first; i < last; i++ {  // 通常 6~8 行
        l.Draw(dc, i, rowRect(i), state)   // 按需取数据渲染
    }
}
```

- 控件数恒定（1 个滚动区），**与评论总数无关**；190 万条评论和 20 条评论占用的控件内存完全一样。
- 行的换行用 `GetTextExtentPoint32W` 二分测量前缀宽度（CJK 无空格，GDI 的 word-break 不可用），最多 2 行，超出加省略号。
- 滚轮/滚动条拖动/键盘 ↑↓ PageUp/PageDown 都只改 `Top`（首个可视行），随后重绘可视区。

**内存代价**：热门页 15 条 + 最新页 20 条 ≈ 7KB 数据 + 1 个固定控件。
实测评论模块增量 **+1.27MB**（主要是界面缓冲与 Go 堆抖动），远低于 20MB 降级阈值，故**未降级**。

**异步性**：评论请求在独立 goroutine 中执行，结果经 `post()` 回到 UI 线程；加载期间界面显示「加载中…」，播放与主界面完全不受影响。

## 2.5 音频管线

```
HTTP(整曲) → []byte(压缩, 1~3MB) → go-mp3 解码(按需) → IAudioRenderClient 缓冲(100ms)
                                        ↑                        │
                                    seek(帧索引, 1ms)      WASAPI 共享模式 → 系统混音器
```

- 设备格式：以解码器输出格式（44.1kHz/16bit/立体声）`Initialize`，并带
  `AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | SRC_DEFAULT_QUALITY`，由音频引擎重采样到设备混音格式
  ——**不需要写任何重采样代码**。
- 位置 = `已写帧数 - 设备内待播帧数`，实测与真实播放进度偏差约 20ms（足够歌词同步）。
- 暂停用 `IAudioClient::Stop/Start`（位置冻结、解码状态保留）；seek 用 `Decoder.Seek`（帧精确）。
- 播放结束由解码 EOF + 缓冲排空判定，回调经 `post()` 回 UI 线程决定下一首（顺序/单曲/随机）。
- 音量在写入设备前就地缩放（`(v/100)²` 曲线），不额外占用内存。

## 2.6 持久化

`%APPDATA%\netmusic\config.json`：Cookie 会话、UID、昵称、音量、播放模式、音质、主窗口坐标、最后歌单。
写入用 `tmp + rename` 原子替换；音量拖动等高频变更走 1.5s 防抖。

## 2.7 与 go-musicfox 的关系

参考了它的**后端思路**（未引入其代码或其终端 UI 层）：

- `weapi` 加密方案：`AES-128-CBC(presetKey) → Base64 → AES-128-CBC(随机16位密钥) → Base64`，随机密钥用**无填充 RSA** 加密成 `encSecKey`。本项目用 `crypto/aes` + `math/big` 重新实现（约 90 行），并且**修掉了它的一个隐患**：原实现 RSA 输出会丢前导零字节，这里补齐到模长，保证 `encSecKey` 恒为 256 hex。
- 端点选择：`/api/song/enhance/player/url`、`/api/song/lyric`、`/api/v1/resource/comments/R_SO_4_*`、`/api/v6/playlist/detail`、`/api/nuser/account/get`、`/api/login/qrcode/*`。本项目优先选用**无需加密的 `/api/` 接口**，因此加密只用于手机号登录一条路径，依赖面积最小。
- 为什么不会破坏内存约束：参考实现依赖 `req/v3`、`cookiejar`、`tcell`、`bubbletea`、`beep` 等，其中终端 UI（tcell）和音频（beep/gopxl）才是内存与 CPU 的大头。本项目一个都没有引入；唯一保留的运行时依赖是纯 Go 的 go-mp3（约 6k 行、无 cgo、无全局状态）。
## 2.8 Windows 7 / 32 位适配要点

- **工具链**：Go 1.20.14（1.21+ 不再支持 Win7）。
- **只用 Vista 起就存在的 API**：WASAPI（Vista+）、`AUTOCONVERTPCM`（Win7+）、`SetProcessDPIAware`（Vista+）、
  `EM_SETCUEBANNER`（Vista+）、`RegGetValueW`（Vista+）、`GetTickCount64`（Vista+）。不依赖 Win8/10 的 `GetDpiForWindow`、
  `SetThreadDpiAwarenessContext` 等。
- **字体**：`Microsoft YaHei`（Vista+），不用 Win8 才有的 `Microsoft YaHei UI`。
- **清单文件**：`app.manifest` 经 windres 编成 `.syso` 参与链接，提供 comctl32 v6（输入框占位提示依赖它）、
  `dpiAware=true` 和 Win7/8/8.1/10 的 `supportedOS` GUID（避免被套用兼容性垫片）。
- **32 位 COM 调用约定**：所有 Win32/COM 都是 stdcall；`REFERENCE_TIME` 这类 64 位参数要按架构拆成 1 或 2 个栈槽
  （`args_386.go` / `args_amd64.go` 的 `comArg64`），`MSG` 结构按架构分别定义（`msg_386.go` / `msg_amd64.go`），
  `SetWindowLongPtr` 运行时探测 `Ptr` 版本是否存在。
- **GUI 子系统**：链接时 `-H windowsgui`，避免双击弹出控制台窗口；代价是命令行采集 stderr 需要显式重定向。
## 2.9 音频双路径（Windows 7 保险）

```
解码器(16bit/44.1k 立体声)
        │
        ├─ 路径 A：IAudioClient.Initialize(我们的格式 + AUTOCONVERTPCM|SRC_DEFAULT_QUALITY)
        │         引擎内部重采样到设备混音格式。CPU 最省，Win7+ 支持。
        │
        └─ 路径 B（A 被拒绝时）：GetMixFormat() → Initialize(设备格式, flags=0)
                 软件把 16bit 转成 16/24/32-bit 整数或 32/64-bit 浮点，
                 采样率不同就用带小数累加器的线性插值（converter）。
```

- 两条路径都在 `player.go` 的同一个循环里，差别只在"往设备缓冲写什么"。
- 路径 B 的实现集中在 `internal/audio/convert.go`（约 130 行），首次填充时按需扩容小缓冲
  （约 20KB），之后稳态零分配。
- 路径 B 平时用不到，所以提供 `-force-audio-fallback`（以及 nmprobe 的 `NETMUSIC_FORCE_FALLBACK=1`）
  强制走过并验证 —— 未经测试的兜底代码等于没有。
- 音量在两条路径上都是就地缩放 int16（`(v/100)²` 曲线），转换发生在缩放之后。