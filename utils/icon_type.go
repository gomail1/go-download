package utils

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 全文件类型图标体系（v1.3.1）
//
// 背景：真实图标只能从「内嵌图标资源的文件」（PE 可执行文件、.ico 等）中提取，
// 普通文件（压缩包/图片/视频/文档…）内部没有图标资源可提取。
// 为让所有文件都有清晰、统一、跨端一致的图标，这里按扩展名将文件归类为若干
// 「类型」，每类对应一枚品牌色圆角底 + 白色线条的矢量图标（SVG）。
// 该图标不携带任何文件内容特征，公开返回无信息泄露风险。
//
// 需要「展示文件真实外观」的（图片缩略图、视频封面帧）属后续可选增强，
// 因会公开预览文件内容，需另行权衡登录策略；视频截帧还依赖 ffmpeg。

// svgWrap 组装一枚 64x64 的彩色底图标：品牌色圆角方块 + 白色图形
func svgWrap(fill, glyph string) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 24 24">` +
		`<rect x="0.8" y="0.8" width="22.4" height="22.4" rx="5" fill="` + fill + `"/>` + glyph + `</svg>`
}

// 各类别的白色图形（24 网格内绘制）
const (
	glyphFolder  = `<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>`
	glyphFile    = `<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><polyline points="14 2 14 8 20 8" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>`
	glyphFileTxt = glyphFile + `<line x1="16" y1="13" x2="8" y2="13" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round"/><line x1="16" y1="17" x2="8" y2="17" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round"/>`
	glyphBox     = `<path d="M12 3.6 20.4 8v8L12 20.4 3.6 16V8Z" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><path d="M3.6 8.2 12 13l8.4-4.8" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>`
	glyphImg     = `<rect x="3" y="3" width="18" height="18" rx="2" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><circle cx="8.5" cy="8.5" r="1.5" fill="none" stroke="#FFFFFF" stroke-width="1.7"/><polyline points="21 15 16 10 5 21" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>`
	glyphVideo   = `<rect x="2" y="5" width="14" height="14" rx="2" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><polygon points="16 10.5 21.5 7.5 21.5 16.5 16 13.5" fill="#FFFFFF"/>`
	glyphAudio   = `<path d="M9 18V5l12-2v13" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><circle cx="6" cy="18" r="3" fill="none" stroke="#FFFFFF" stroke-width="1.7"/><circle cx="18" cy="16" r="3" fill="none" stroke="#FFFFFF" stroke-width="1.7"/>`
	glyphCode    = `<polyline points="16 18 22 12 16 6" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><polyline points="8 6 2 12 8 18" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/>`
	glyphDL      = `<path d="M21 15v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-3" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><polyline points="7 10 12 15 17 10" fill="none" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><line x1="12" y1="15" x2="12" y2="3" stroke="#FFFFFF" stroke-width="1.7" stroke-linecap="round"/>`
	glyphDisc    = `<circle cx="12" cy="12" r="8.5" fill="none" stroke="#FFFFFF" stroke-width="1.7"/><circle cx="12" cy="12" r="3" fill="none" stroke="#FFFFFF" stroke-width="1.7"/>`
	glyphDB      = `<ellipse cx="12" cy="5.5" rx="8.5" ry="3" fill="none" stroke="#FFFFFF" stroke-width="1.7"/><path d="M20.5 12c0 1.66-3.8 3-8.5 3s-8.5-1.34-8.5-3" fill="none" stroke="#FFFFFF" stroke-width="1.7"/><path d="M3.5 5.5v13c0 1.66 3.8 3 8.5 3s8.5-1.34 8.5-3v-13" fill="none" stroke="#FFFFFF" stroke-width="1.7"/>`
	glyphRun     = `<polygon points="9 6.8 16.5 12 9 17.2" fill="#FFFFFF"/>`
)

// iconTypeMap：类别 → 图标（品牌色 + 图形）
var iconTypeMap = map[string]string{
	"folder":  svgWrap("#F59E0B", glyphFolder),
	"program": svgWrap("#4F46E5", glyphRun),
	"archive": svgWrap("#B45309", glyphBox),
	"pdf":     svgWrap("#DC2626", glyphFileTxt),
	"word":    svgWrap("#2563EB", glyphFileTxt),
	"excel":   svgWrap("#16A34A", glyphFileTxt),
	"ppt":     svgWrap("#EA580C", glyphFileTxt),
	"image":   svgWrap("#0D9488", glyphImg),
	"video":   svgWrap("#7C3AED", glyphVideo),
	"audio":   svgWrap("#DB2777", glyphAudio),
	"code":    svgWrap("#334155", glyphCode),
	"seed":    svgWrap("#F43F5E", glyphDL),
	"iso":     svgWrap("#64748B", glyphDisc),
	"db":      svgWrap("#0891B2", glyphDB),
	"text":    svgWrap("#6B7280", glyphFileTxt),
	"file":    svgWrap("#9CA3AF", glyphFile),
}

// iconTypeByExt：扩展名 → 类别
func fileIconType(ext string) string {
	switch ext {
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".bz2", ".xz", ".tgz", ".zst", ".cab", ".lz4":
		return "archive"
	case ".pdf":
		return "pdf"
	case ".doc", ".docx", ".docm", ".dot", ".dotx":
		return "word"
	case ".xls", ".xlsx", ".xlsm", ".xlsb", ".csv", ".tsv":
		return "excel"
	case ".ppt", ".pptx", ".pps", ".ppsx", ".pot", ".potx":
		return "ppt"
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".tif", ".tiff", ".heic", ".heif", ".avif", ".raw", ".psd", ".ai", ".eps":
		return "image"
	case ".mp4", ".avi", ".mkv", ".mov", ".wmv", ".flv", ".webm", ".mpeg", ".mpg", ".rm", ".rmvb", ".m4v", ".3gp", ".mts", ".m2ts":
		return "video"
	case ".mp3", ".wav", ".flac", ".aac", ".ogg", ".wma", ".m4a", ".ape", ".opus", ".mid", ".midi", ".amr", ".aiff":
		return "audio"
	case ".py", ".go", ".js", ".mjs", ".ts", ".tsx", ".jsx", ".java", ".c", ".cc", ".cpp", ".h", ".hpp", ".php", ".rb", ".swift", ".kt", ".kts", ".scala", ".rs", ".lua", ".sql", ".vue", ".svelte", ".css", ".scss", ".less", ".html", ".htm", ".cs", ".dart", ".r", ".pl", ".sh", ".ps1":
		return "code"
	case ".torrent":
		return "seed"
	case ".iso", ".img", ".dmg", ".vhd", ".vhdx", ".wim", ".esd", ".vmdk":
		return "iso"
	case ".db", ".sqlite", ".sqlite3", ".mdb", ".accdb", ".dbf":
		return "db"
	case ".bat", ".cmd", ".com", ".reg", ".jar", ".apk", ".app", ".deb", ".rpm", ".pkg":
		return "program"
	case ".txt", ".md", ".markdown", ".log", ".rtf", ".ini", ".conf", ".cfg", ".json", ".yaml", ".yml", ".xml", ".toml", ".properties", ".tex", ".nfo", ".srt", ".ass", ".url", ".ttf", ".otf", ".woff", ".woff2", ".eot":
		return "text"
	default:
		return "file"
	}
}

// GetFileTypeIconSVG 返回某文件/目录对应的彩色类型图标 SVG。
// isDir=true 时固定返回文件夹图标（调用方先判断 os 目录）。
func GetFileTypeIconSVG(ext string, isDir bool) string {
	if isDir {
		return iconTypeMap["folder"]
	}
	t := fileIconType(strings.ToLower(ext))
	if svg, ok := iconTypeMap[t]; ok {
		return svg
	}
	return iconTypeMap["file"]
}

// ---------------------------------------------------------------------------
// 分类图标：与文件类型图标共用同一套图形（同源），分类 Icon 字段存「类型键」而非 emoji。
// ---------------------------------------------------------------------------

// iconTypeKeys 图标的稳定展示顺序（分类管理选择器等界面共用）
var iconTypeKeys = []string{
	"folder", "program", "archive", "pdf", "word", "excel", "ppt",
	"image", "video", "audio", "code", "seed", "iso", "db", "text", "file",
}

// iconTypeColor 各类型键 → 品牌底色
var iconTypeColor = map[string]string{
	"folder":  "#F59E0B",
	"program": "#4F46E5",
	"archive": "#B45309",
	"pdf":     "#DC2626",
	"word":    "#2563EB",
	"excel":   "#16A34A",
	"ppt":     "#EA580C",
	"image":   "#0D9488",
	"video":   "#7C3AED",
	"audio":   "#DB2777",
	"code":    "#334155",
	"seed":    "#F43F5E",
	"iso":     "#64748B",
	"db":      "#0891B2",
	"text":    "#6B7280",
	"file":    "#9CA3AF",
}

// iconTypeLabel 各类型键 → 中文名（管理界面/悬停提示）
var iconTypeLabel = map[string]string{
	"folder":  "文件夹",
	"program": "程序/软件",
	"archive": "压缩包",
	"pdf":     "PDF",
	"word":    "Word",
	"excel":   "Excel",
	"ppt":     "PPT",
	"image":   "图片",
	"video":   "视频",
	"audio":   "音频",
	"code":    "代码",
	"seed":    "种子",
	"iso":     "系统镜像",
	"db":      "数据库",
	"text":    "文本",
	"file":    "未知文件",
}

// categoryIconAlias 旧版本分类存储的 emoji → 类型键（自动迁移用）。
// 对应默认五分类：📁全部、💿常用软件、💻系统镜像、📦压缩包、📄办公文档。
var categoryIconAlias = map[string]string{
	"📁": "folder", "📂": "folder",
	"💿": "program", "📀": "program", "🖥": "program", "🧰": "program", "⚙️": "program", "🤖": "program", "🛠️": "program",
	"💻": "iso",
	"📦": "archive", "🗜️": "archive", "🗃️": "archive",
	"📄": "word", "📝": "word", "📃": "word", "📑": "word", "✏️": "word",
	"🖼️": "image", "📷": "image", "🎨": "image",
	"🎬": "video", "🎥": "video", "📺": "video",
	"🎵": "audio", "🎧": "audio", "🎶": "audio",
	"🔧": "code", "🧑💻": "code", "👨💻": "code",
	"🧲": "seed",
	"💾": "db", "🗄️": "db",
	"📋": "text", "📒": "text",
	"❓": "file", "📎": "file", "📌": "file",
}

// NormalizeCategoryIcon 将分类图标值规范化为合法的类型键。
// 兼容：已是类型键 → 原样返回；旧版 emoji → 映射到对应键；空/未知 → folder。
func NormalizeCategoryIcon(icon string) string {
	if icon == "" {
		return "folder"
	}
	if _, ok := iconTypeColor[icon]; ok {
		return icon
	}
	if k, ok := categoryIconAlias[icon]; ok {
		return k
	}
	return "folder"
}

// GetCategoryIconSVG 返回指定大小（px）的分类图标 SVG（key 自动规范化）。
func GetCategoryIconSVG(key string, size int) string {
	key = NormalizeCategoryIcon(key)
	svg := iconTypeMap[key]
	return strings.Replace(svg, `width="64" height="64"`, fmt.Sprintf(`width="%d" height="%d"`, size, size), 1)
}

// CategoryIconKeys 返回类型键的展示顺序（前端选择器注入用）。
func CategoryIconKeys() []string {
	out := make([]string, len(iconTypeKeys))
	copy(out, iconTypeKeys)
	return out
}

// CategoryIconLabel 返回类型键的中文名（未知键回落 folder）。
func CategoryIconLabel(key string) string {
	return iconTypeLabel[NormalizeCategoryIcon(key)]
}

// CategoryIconMetaJSON 返回分类图标元数据 JSON（管理端图标选择器注入用）：
// [{"key":"folder","label":"文件夹"}, ...]，顺序同展示顺序。
func CategoryIconMetaJSON() string {
	type item struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	list := make([]item, 0, len(iconTypeKeys))
	for _, k := range iconTypeKeys {
		list = append(list, item{Key: k, Label: iconTypeLabel[k]})
	}
	b, _ := json.Marshal(list)
	return string(b)
}

// CategoryIconSpriteHTML 生成内联 SVG sprite：每个类型图标一个 <g id="ct_<key>">，
// 供页面 JS 通过 <use href="#ct_<key>"> 引用（管理端分类图标选择器/列表用）。
func CategoryIconSpriteHTML() string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0" style="position:absolute" aria-hidden="true"><defs>`)
	for _, k := range iconTypeKeys {
		inner := iconTypeMap[k]
		if i := strings.IndexByte(inner, '>'); i > 0 {
			inner = inner[i+1:]
		}
		inner = strings.TrimSuffix(inner, `</svg>`)
		b.WriteString(`<g id="ct_` + k + `">` + inner + `</g>`)
	}
	b.WriteString(`</defs></svg>`)
	return b.String()
}
