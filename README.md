# 网易云音乐 · MyGo（第三方音源改造版）

fork 自 [egoist/mygo-cloudmusic](https://github.com/egoist/mygo-cloudmusic)，基于上游 `2b6673e`。
在保留上游全部功能的前提下，这一版加了第三方音源、本地音乐页、下载、歌词时间偏移与窗口置顶。
[上游原本的介绍原样保留在文末](#上游原版介绍)。

---

## 一、这个版本加了什么

### 1. 第三方音源

网易云给不出音频流的歌，交给第三方音源取流。

- 支持四类音源：洛雪（LX）JS 脚本、HTTP 接口、歌单链接、本机文件夹
- 音源选择器：搜索框右侧的漏斗，逐个勾选参与搜索的音源
- 地址里带 `name` / `keywords` / `q` 参数也能搜，会自动替换该参数的值，不用手写 `{keyword}`

### 2. 本地音乐页

- MP3 / FLAC / OGG / WAV 纯 Go 解码，不依赖任何外部程序
- 装了 ffmpeg 时，WMA / M4A / AAC / OPUS / APE 等也能播
- 封面与歌词按「歌名 + 歌手」自动补齐；带括号的版本后缀会先去括号再搜

### 3. 下载

- 列表每行悬停出现下载按钮，点一下从最高音质逐级降级，稳定落盘
- 播放页可选音质
- 文件连同封面、歌词一起写入下载目录

### 4. 歌词时间偏移

- 窗口右侧竖排箭头，上下各一个，中间只显示秒数，没有文字标签
- 每次 ±0.1 秒，范围 ±10 秒，现场版、翻录版对不上时用

### 5. 窗口置顶

- 顶栏右上角钉子按钮，实心表示已置顶，再点一下取消

## 二、截图

第三方音源页：搜索框回车即搜「网上」，右侧漏斗切换音源（洛雪脚本 / HTTP 接口 / 歌单链接 / 本机文件夹）。

![第三方音源页与音源选择器](docs/third-party-sources/01-third-party-source-search.png)

音源选择器展开：逐个勾选参与搜索的音源。

![音源选择器菜单](docs/third-party-sources/02-source-picker-menu.png)

本机音乐页：扫描后按格式打标记，能播的直接播，缺封面歌词的按「歌名 + 歌手」自动补齐。

![本地音乐页与格式标记](docs/third-party-sources/03-local-music-formats.png)

列表每行悬停出现下载按钮，点一下就从最高音质往下试，文件连同封面、歌词一起落盘。

![列表每行的下载按钮](docs/third-party-sources/07-list-row-download.png)

歌词对不上时，窗口右侧竖排箭头逐 0.1 秒微调。

![歌词时间偏移 +0.4 秒](docs/third-party-sources/05-lyrics-offset-plus-0.4.png)

顶栏右上角的置顶开关，实心表示已置顶，再点一下取消。

![顶栏置顶按钮](docs/third-party-sources/06-titlebar-pin-button.png)

更多细节见 [docs/third-party-sources/features.md](docs/third-party-sources/features.md)。

## 三、下载

最新 Windows 版：[Releases · windows-0.1.24](https://github.com/qq1131909103/mygo-cloudmusic/releases/tag/windows-0.1.24)

| 附件 | 说明 |
| --- | --- |
| `MyGo-CloudMusic-0.1.24-windows-amd64.zip` | Windows 安装包（含 `install.ps1` 与安装器 exe） |
| `MyGo-CloudMusic-0.1.24-source.zip` | 完整源码快照 |
| `MyGo-CloudMusic-0.1.24-third-party-sources.patch` | 相对上游 `2b6673e` 的补丁 |
| `FEATURES.md` / `FEATURES-screenshots.zip` | 图文说明与截图 |
| `SHA256SUMS` | 全部附件的校验值 |

## 四、改动后的验证

```
go build ./...   → 0
go vet ./...     → 0
gofmt（167 文件）→ 全部干净
go test ./...    → 全部通过
```

## 五、本次改造新增的依赖

- [goja](https://github.com/dop251/goja)：洛雪音源脚本的 JS 运行时
- [oggvorbis](https://github.com/jfreymuth/oggvorbis)：Ogg Vorbis 解码
- [FFmpeg](https://ffmpeg.org)：WMA / M4A 等格式的解码，由本机已装的 ffmpeg 提供

## 六、贡献者

- [@qq1131909103](https://github.com/qq1131909103)：第三方音源、本地音乐、下载、歌词偏移、窗口置顶
- **DeepSeek**：参与上述功能的实现与测试

---

## 上游原版介绍

> 以下内容来自上游 [egoist/mygo-cloudmusic](https://github.com/egoist/mygo-cloudmusic) 的 README，原样保留。

### 网易云音乐 · MyGo

用 Go 和 [MyGo](https://github.com/egoist/mygo) 写的第三方网易云音乐桌面客户端，原生界面，无 WebView。

#### 功能

- 扫码登录，登录态保存在系统凭据存储中
- 每日推荐、推荐歌单、排行榜、我的歌单
- 搜索单曲、歌手、专辑、用户与播客，输入时显示搜索建议，保留搜索历史
- 歌手页的热门与最新歌曲、专辑与动态，关注歌手；专辑页与用户页
- 播客：浏览与收听节目，订阅播客；在本机把节目转写成字幕（Qwen3-ASR），像歌词一样随播放滚动
- 私人 FM（推荐 / 熟悉 / 探索）与心动模式
- 边下边播，支持标准 / 较高 / 极高 / 无损（FLAC）/ Hi-Res 音质
- 听过的歌曲缓存在本机，再次播放无需等待
- 音量均衡，暂停与继续时淡出淡入，拖动进度无杂音
- 退出后再打开，从上次的播放队列与进度继续
- 逐字歌词与翻译，正在播放页的「律动」视图与歌曲评论
- 刘海播放器：在屏幕顶部刘海处显示迷你播放器，悬停展开（在设置中开启）
- 喜欢歌曲、播放队列、随机与循环播放
- 浅色 / 深色 / 跟随系统

#### 构建

需要 Go 1.27+。

```bash
go tool mygo dev     # 开发模式运行
go tool mygo build   # 打包到 build/
go test ./...        # 运行测试
```

#### 致谢

- [MyGo](https://github.com/egoist/mygo)：原生 UI 与应用框架
- [NeteaseCloudMusicApi](https://github.com/Binaryify/NeteaseCloudMusicApi)：eapi 加密与登录流程的参考
- [folia-major](https://github.com/chthollyphile/folia-major)：歌词动画效果的参考
- [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx)、[Qwen3-ASR](https://github.com/QwenLM/Qwen3-ASR) 与 [Silero VAD](https://github.com/snakers4/silero-vad)：本机语音识别

本项目与网易云音乐官方无关，音乐版权归各权利人所有。

#### 许可证

[AGPL-3.0](LICENSE)
