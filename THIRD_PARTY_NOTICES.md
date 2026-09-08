# THIRD-PARTY NOTICES — go-download v1.3.0

> 本项目（go-download v1.3.0）自身采用 **MIT License**（见仓库根目录 `LICENSE`，Copyright (c) 2026 gomail1）。
> 本文件列出对外发行物（Docker 镜像 / GitHub Release / 交叉编译二进制）中包含或引用的全部第三方开源组件及其许可声明。
> 依据各许可证条款，随发行物保留下述版权与许可声明；**各许可证全文归档于本文件同目录 `licenses/`**（Docker 镜像内路径：`/app/THIRD_PARTY_NOTICES.md`、`/app/licenses/`）。

- 生成日期：2026-09-09
- 依赖基线：`go.mod` / `go.sum`（`go list -m all` 全量 144 个模块）+ 静态资源审计
- 许可汇总：MIT 81 · Apache-2.0 21 · BSD-3-Clause 26 · BSD-2-Clause 2 · ISC 3 · MPL-2.0 10 · MIT(随包缺 LICENSE) 1
- **无 GPL / AGPL / LGPL / EPL / SSPL 依赖**

---

## 1. 直接依赖（源码 import，编译链接进二进制，共 10 个）

| 模块 | 版本 | 许可证 | 版权持有人（依 LICENSE 原文） |
|---|---|---|---|
| github.com/anacrolix/torrent | v1.61.0 | MPL-2.0 | Mozilla Public License 2.0 |
| github.com/charmbracelet/bubbles | v1.0.0 | MIT | Copyright (c) 2020-2025 Charmbracelet, Inc |
| github.com/charmbracelet/bubbletea | v1.3.10 | MIT | Copyright (c) 2020-2025 Charmbracelet, Inc |
| github.com/fsnotify/fsnotify | v1.10.1 | BSD-3-Clause | Copyright © fsnotify Authors |
| github.com/gin-gonic/gin | v1.12.0 | MIT | Copyright (c) 2014 Manuel Martínez-Almeida |
| github.com/gorilla/websocket | v1.5.3 | BSD-2-Clause | Copyright (c) 2013 The Gorilla WebSocket Authors |
| github.com/sirupsen/logrus | v1.10.2 | MIT | Copyright (c) 2014 Simon Eskildsen |
| github.com/spf13/viper | v1.21.0 | MIT | Copyright (c) 2014 Steve Francia |
| github.com/vtemlabs/imaging | v1.6.4 | MIT | Copyright (c) 2012 Grigory Dryapak（fork of disintegration/imaging） |
| golang.org/x/crypto | v0.56.0 | BSD-3-Clause | Copyright 2009 The Go Authors |

> 用途：anacrolix/torrent=BT/磁力下载引擎；gin + gorilla/websocket=HTTP 框架与 WebSocket；charmbracelet/bubbles+bubbletea=下载任务 TUI；fsnotify=文件监听；logrus=日志；viper=配置；vtemlabs/imaging=图片处理；x/crypto=bcrypt 口令哈希。

---

## 2. 传递依赖（共 134 个，按许可证分组）

### 2.1 MPL-2.0（9 个）

| 模块 | 版本 |
|---|---|
| github.com/anacrolix/dht/v2 | v2.23.0 |
| github.com/anacrolix/generics | v0.2.0 |
| github.com/anacrolix/log | v0.17.1-0.20251118025802-918f1157b7bb |
| github.com/anacrolix/mmsg | v1.1.1 |
| github.com/anacrolix/multiless | v0.4.0 |
| github.com/anacrolix/sync | v0.6.0 |
| github.com/anacrolix/upnp | v0.1.4 |
| github.com/anacrolix/utp | v0.2.0 |
| github.com/go-llsqlite/adapter | v0.2.0 |

### 2.2 MIT（75 个）

| 模块 | 版本 |
|---|---|
| github.com/alecthomas/atomic | v0.1.0-alpha2 |
| github.com/anacrolix/chansync | v0.7.0 |
| github.com/anacrolix/envpprof | v1.5.0 |
| github.com/anacrolix/go-libutp | v1.3.2 |
| github.com/anacrolix/missinggo | v1.3.0 |
| github.com/anacrolix/missinggo/perf | v1.0.0 |
| github.com/anacrolix/missinggo/v2 | v2.10.0 |
| github.com/anacrolix/stm | v0.5.0 |
| github.com/aymanbagabas/go-osc52/v2 | v2.0.1 |
| github.com/benbjohnson/immutable | v0.4.3 |
| github.com/cespare/xxhash | v1.1.0 |
| github.com/cespare/xxhash/v2 | v2.3.0 |
| github.com/charmbracelet/colorprofile | v0.4.3 |
| github.com/charmbracelet/harmonica | v0.2.0 |
| github.com/charmbracelet/lipgloss | v1.1.0 |
| github.com/charmbracelet/x/ansi | v0.11.8 |
| github.com/charmbracelet/x/cellbuf | v0.0.15 |
| github.com/charmbracelet/x/term | v0.2.2 |
| github.com/clipperhouse/displaywidth | v0.11.0 |
| github.com/clipperhouse/uax29/v2 | v2.7.0 |
| github.com/dustin/go-humanize | v1.0.1 |
| github.com/erikgeiser/coninput | v0.0.0-20211004153227-1c3628e74d0f |
| github.com/felixge/fgprof | v0.9.5 |
| github.com/gabriel-vasile/mimetype | v1.4.15 |
| github.com/gin-contrib/sse | v1.1.2 |
| github.com/go-playground/locales | v0.14.1 |
| github.com/go-playground/universal-translator | v0.18.1 |
| github.com/go-playground/validator/v10 | v10.30.4 |
| github.com/go-viper/mapstructure/v2 | v2.5.0 |
| github.com/goccy/go-json | v0.10.6 |
| github.com/goccy/go-yaml | v1.19.2 |
| github.com/huandu/xstrings | v1.5.0 |
| github.com/json-iterator/go | v1.1.12 |
| github.com/klauspost/cpuid/v2 | v2.4.0 |
| github.com/leodido/go-urn | v1.5.0 |
| github.com/lucasb-eyer/go-colorful | v1.1.1 |
| github.com/mattn/go-isatty | v0.0.24 |
| github.com/mattn/go-runewidth | v0.0.29 |
| github.com/mr-tron/base58 | v1.3.0 |
| github.com/muesli/ansi | v0.0.0-20230316100256-276c6243b2f6 |
| github.com/muesli/cancelreader | v0.2.2 |
| github.com/muesli/termenv | v0.16.0 |
| github.com/multiformats/go-multihash | v0.2.3 |
| github.com/multiformats/go-varint | v0.1.0 |
| github.com/ncruces/go-strftime | v1.0.0 |
| github.com/pelletier/go-toml/v2 | v2.4.3 |
| github.com/pion/datachannel | v1.6.2 |
| github.com/pion/dtls/v3 | v3.1.8 |
| github.com/pion/ice/v4 | v4.4.2 |
| github.com/pion/interceptor | v0.1.48 |
| github.com/pion/logging | v0.2.4 |
| github.com/pion/mdns/v2 | v2.2.0 |
| github.com/pion/randutil | v0.1.0 |
| github.com/pion/rtcp | v1.2.17 |
| github.com/pion/rtp | v1.10.5 |
| github.com/pion/sctp | v1.11.1 |
| github.com/pion/sdp/v3 | v3.0.19 |
| github.com/pion/srtp/v3 | v3.0.13 |
| github.com/pion/stun/v4 | v4.0.0 |
| github.com/pion/transport/v4 | v4.1.0 |
| github.com/pion/turn/v5 | v5.1.0 |
| github.com/pion/webrtc/v4 | v4.2.20 |
| github.com/protolambda/ctxlock | v0.1.0 |
| github.com/quic-go/qpack | v0.6.0 |
| github.com/quic-go/quic-go | v0.62.0 |
| github.com/rivo/uniseg | v0.4.7 |
| github.com/rs/dnscache | v0.0.0-20230804202142-fc85eb664529 |
| github.com/sagikazarmark/locafero | v0.12.0 |
| github.com/spf13/cast | v1.10.0 |
| github.com/subosito/gotenv | v1.6.0 |
| github.com/tidwall/btree | v1.8.1 |
| github.com/ugorji/go/codec | v1.2.6 |
| github.com/xo/terminfo | v1.0.0 |
| go.etcd.io/bbolt | v1.5.0 |
| lukechampine.com/blake3 | v1.4.1 |

### 2.3 Apache-2.0（21 个）

| 模块 | 版本 |
|---|---|
| github.com/RoaringBitmap/roaring | v1.9.4 |
| github.com/anacrolix/btree | v0.1.1 |
| github.com/bytedance/gopkg | v0.1.4 |
| github.com/bytedance/sonic | v1.15.3 |
| github.com/bytedance/sonic/loader | v0.5.2 |
| github.com/cloudwego/base64x | v0.1.7 |
| github.com/go-logr/logr | v1.4.4 |
| github.com/go-logr/stdr | v1.2.2 |
| github.com/google/btree | v1.1.3 |
| github.com/google/pprof | v0.0.0-20260903180319-d6c3cb2f37ec |
| github.com/minio/sha256-simd | v1.0.1 |
| github.com/modern-go/concurrent | v0.0.0-20180306012644-bacd9c7ef1dd |
| github.com/modern-go/reflect2 | v1.0.2 |
| github.com/mschoch/smat | v0.2.0 |
| github.com/spf13/afero | v1.15.0 |
| go.mongodb.org/mongo-driver/v2 | v2.9.0 |
| go.opentelemetry.io/auto/sdk | v1.2.1 |
| go.opentelemetry.io/otel | v1.46.0 |
| go.opentelemetry.io/otel/metric | v1.46.0 |
| go.opentelemetry.io/otel/trace | v1.46.0 |
| go.yaml.in/yaml/v3 | v3.0.5 |

### 2.4 BSD-3-Clause（24 个）

| 模块 | 版本 |
|---|---|
| github.com/bahlo/generic-list-go | v0.2.0 |
| github.com/bits-and-blooms/bitset | v1.25.0 |
| github.com/bradfitz/iter | v0.0.0-20191230175014-e8f45d346db8 |
| github.com/edsrzf/mmap-go | v1.2.0 |
| github.com/google/go-cmp | v0.7.0 |
| github.com/google/uuid | v1.6.0 |
| github.com/remyoudompheng/bigfft | v0.0.0-20230129092748-24d4a6f8daec |
| github.com/spaolacci/murmur3 | v1.1.0 |
| github.com/spf13/pflag | v1.0.10 |
| github.com/twitchyliquid64/golang-asm | v0.15.1 |
| github.com/wlynxg/anet | v0.0.5 |
| golang.org/x/arch | v0.30.0 |
| golang.org/x/exp | v0.0.0-20260824195058-e88cd73687aa |
| golang.org/x/image | v0.45.0 |
| golang.org/x/net | v0.58.0 |
| golang.org/x/sync | v0.22.0 |
| golang.org/x/sys | v0.47.0 |
| golang.org/x/text | v0.41.0 |
| golang.org/x/time | v0.15.0 |
| google.golang.org/protobuf | v1.36.12 |
| modernc.org/libc | v1.75.7 |
| modernc.org/mathutil | v1.7.1 |
| modernc.org/memory | v1.12.1 |
| modernc.org/sqlite | v1.58.0 |

### 2.5 BSD-2-Clause（1 个）

| 模块 | 版本 |
|---|---|
| github.com/pkg/errors | v0.9.1 |

### 2.6 ISC（3 个）

| 模块 | 版本 |
|---|---|
| github.com/davecgh/go-spew | v1.1.1 |
| github.com/go-llsqlite/crawshaw | v0.6.0 |
| zombiezen.com/go/sqlite | v1.4.2 |

### 2.7 MIT — 随包缺 LICENSE 文件（1 个）

| 模块 | 版本 | 说明 |
|---|---|---|
| github.com/mattn/go-localereader | v0.0.1 | 上游打包缺陷：模块包内未含 LICENSE 文件；其 GitHub 仓库以 MIT 发布，本声明按 MIT 记录 |

---

## 3. Vendored / 静态资源

| 资源 | 版本 | 许可证 | 版权声明 | 归档位置 |
|---|---|---|---|---|
| static/lib/chart.umd.min.js（Chart.js） | 4.4.0 | MIT | (c) 2023 Chart.js Contributors | `static/lib/LICENSE.chartjs` |

---

## 4. 合规说明与持续义务

1. **MPL-2.0 边界**：本项目以「未修改源码、库引用」方式使用 anacrolix/torrent 系模块，按 MPL-2.0 聚合/独立作品条款**不强制本项目开源**；对外分发二进制时随附 MPL-2.0 文本（见 `licenses/MPL-2.0.txt`）即满足义务。若未来直接修改任何 MPL-2.0 文件，该等文件须以 MPL-2.0 继续开源并提供对应源码。
2. **Apache-2.0**：随发行物保留 `licenses/Apache-2.0.txt` 并满足「保留 NOTICE（如有）」要求（各模块均未附带 NOTICE 文件）。
3. **MIT / BSD / ISC**：随发行物保留本清单及对应许可证全文即满足「保留版权与许可声明」义务。
4. 本项目 66 个 `.go` 源文件均已在文件头标注 `// SPDX-License-Identifier: MIT` 与 `// Copyright (c) 2026 gomail1`。

---

*本清单依据 go.mod/go.sum 与模块缓存内 LICENSE 原文核读生成，与《开源合规审计报告》同源。依赖变动后请重新生成本文件。*
