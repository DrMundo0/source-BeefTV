# 媒体组件的许可与源码交付

BeefTV 保留现有 FFmpeg 媒体能力。根目录 `LICENSE` 中的新源码可用条款仅适用于其约定的新增原创内容；历史 MIT 内容及第三方组件继续适用原许可证。不能用一条额外声明覆盖第三方许可证。

## 组件与调用方式

| 组件 | 版本与许可 | 使用方式 |
| --- | --- | --- |
| 浏览器 `@ffmpeg/core` | 0.12.10，GPL-2.0-or-later | Worker 中运行，提供剪辑、裁剪、合并、音频处理及导出 |
| Windows FFmpeg/ffprobe | Gyan 8.0.1 essentials，GPL-3.0-or-later | 独立命令行进程，通过参数和文件交换数据 |
| macOS ffprobe | FFmpeg 8.0.1，LGPL-2.1-or-later | 从固定源码编译，关闭 GPL 特性与外部库，独立进程探测媒体 |
| Windows 可选深度运行包 | 主 FFmpeg/ffprobe，以及 imageio-ffmpeg wheel 内的另一个 FFmpeg | Python 启动独立进程；wheel 自带二进制需单独核对 |

原生命令行工具和浏览器 WASM 的使用方式不同，需要分别判断组合分发的许可要求。独立进程、Worker 或单独下载均不能自动证明兼容；第三方清单也不能替代对应源码交付。

## 已实现的交付

- 应用版本号 → 更新日志 → **第三方许可与源码**，可查看项目许可、第三方清单和 GPL/LGPL 全文。
- Web 构建将这些文件写入 `/licenses/`；桌面包同时放在 `agent-host/licenses/`，macOS 位于应用的 `Contents/Resources/` 下，可离线读取。
- `sources.json` 记录本次构建的平台、组件版本、许可和 WASM 文件哈希。`upstream` 是上游项目入口；`source` 仅记录本流水线实际生成的源码包，缺失时为 `null`，页面不生成不存在的下载链接。
- macOS 构建 ffprobe 时同时生成 `BeefTV-vX.Y.Z-ffprobe-darwin-{arm64,amd64}-sources.tar.gz`，包含未经修改的源码、许可证、实际构建脚本、参数和二进制哈希。正式桌面流水线将它与安装包一同上传到同版本 GitHub Release。
- 浏览器核心的源码包 `BeefTV-vX.Y.Z-ffmpeg-wasm-sources.tar.gz` 包含 21 份固定提交的源码归档、许可证、上游编译脚本和离线构建说明。正式桌面流水线先生成源码包，再将其下载信息写入各平台许可页，并与安装包一起上传。普通本地/Web 构建只在实际提供该源码包时写入下载信息。

可单独生成 macOS 源码包：

```sh
bash scripts/package-media-runtime-macos.sh darwin/arm64 ./media-runtime \
  ./ffmpeg-source.tar.gz ./BeefTV-vX.Y.Z-ffprobe-darwin-arm64-sources.tar.gz
```

这里记录哈希用于核对材料，不将工程校验等同于法律兼容性结论。

生成或检查 WASM 源码包：

```sh
python3 scripts/package-wasm-sources.py --version vX.Y.Z \
  --binary-dir web/node_modules/@ffmpeg/core/dist/esm
python3 scripts/package-wasm-sources.py \
  --verify dist/BeefTV-vX.Y.Z-ffmpeg-wasm-sources.tar.gz
```

固定版本清单在 `licenses/ffmpeg-wasm-sources.json`。源码选择依据上游 0.12.10 发布提交、依赖历史以及现有核心报告的版本：FFmpeg 5.1.4、Emscripten 3.1.40 的精确提交、x264 core 164 和 LAME 3.100 均吻合。源码包保留 `build/*.sh`、修改过的 `src/fftools` 和绑定代码；离线构建配方仅将上游下载步骤改为使用已归档文件。上游未提供签名构建证明，源码包不声称能逐字节复现 npm 产物。自行重编译并替换核心是另一个需要功能验证的操作。

## 发布责任

分发者应保留各组件的署名、许可和适用的源码材料，并提供与所分发二进制对应的源码、修改和必要构建脚本。上游主页或版本号本身不构成完整对应源码的交付。

Windows 预构建工具及 imageio wheel 内嵌 FFmpeg 的依赖源码、补丁和构建材料仍需分别取得；不能用 FFmpeg 主源码或 imageio Python 包的 BSD 许可替代。源码包归档也不自动证明组合分发的许可相容性。本次源码归档不替换编码器。

## 参考

- [FFmpeg 许可说明](https://ffmpeg.org/legal.html)
- [ffmpeg.wasm 官方许可问答](https://ffmpegwasm.netlify.app/docs/faq/)
- [Gyan Windows 构建说明](https://www.gyan.dev/ffmpeg/builds/)
- [Remotion 第三方组件与 FFmpeg 说明](https://www.remotion.dev/docs/acknowledgements)
