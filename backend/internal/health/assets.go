package health

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// webdist 是插件 web 构建产物，由 Makefile 在 build 前从 web/dist 复制过来。
// 用 all: 前缀保证同时 embed 隐藏文件（包括占位的 placeholder）。
//
//go:embed all:webdist
var webDistFS embed.FS

// GetWebAssets 实现 sdk.WebAssetsProvider。
//
// 与 epay 完全相同的双模式：
//   - 开发模式：优先读 ../web/dist 或 web/dist 真实目录
//   - 生产模式：使用 //go:embed 嵌入的 webdist 内容
//
// core 启动插件后会调用此方法，把所有文件写到 data/plugins/airgate-health/assets/。
// 注意：core 通过 r.Static("/plugins", pluginDir) 把这些资源暴露成 /plugins/airgate-health/assets/index.js
// 等供 admin UI 的 plugin-loader 抓取（用于 FrontendPages 中声明的页面）。
//
// 而公开状态页 (/status) 走的是另一条路径：core 用反向代理把 /status/* 转给本插件，
// 本插件的 handlePublicAsset/handlePublicIndex 直接读 readAsset() 返回。
// 因此 webdist 里需要有：
//   - index.js / index.css 等：供 admin UI 加载（FrontendPages）
//   - status.html / status/* 等：供 public 状态页使用
func (p *Plugin) GetWebAssets() map[string][]byte {
	if assets := loadDevAssets(); len(assets) > 0 {
		return assets
	}
	assets := make(map[string][]byte)
	_ = fs.WalkDir(webDistFS, "webdist", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		content, err := webDistFS.ReadFile(path)
		if err != nil {
			return nil
		}
		rel := strings.TrimPrefix(path, "webdist/")
		if rel == "" || rel == ".gitkeep" {
			return nil
		}
		assets[rel] = content
		return nil
	})
	return assets
}

// readAsset 公开状态页 handler 用：按相对路径取一个 webdist 文件。
//
// 双模式：
//   - 开发模式：每次直接读 ../web/dist 真实目录，不缓存。让 vite watch 改完
//     status page 后浏览器硬刷新即可生效，不用重启 health 进程。
//   - 生产模式：使用 //go:embed 嵌入的 webdist 内容，全量加载到内存只一次。
//
// 选择"dev 模式不缓存"是因为 vite watch 重新构建时 dist/ 文件会被瞬间替换，
// 而不是写入到一个新的 hashed 路径——同名文件需要每次都重读。
func (p *Plugin) readAsset(rel string) ([]byte, bool) {
	rel = strings.TrimPrefix(rel, "/")
	if dev := loadDevAssets(); len(dev) > 0 {
		// dev 模式：每次都重新 walk dist/，不复用上次结果
		data, ok := dev[rel]
		return data, ok
	}
	cache := getEmbedAssetCache()
	if data, ok := cache[rel]; ok {
		return data, true
	}
	return nil, false
}

// 生产模式 embed 资源缓存：第一次调用时全量读到内存。
// dev 模式不走这条路径。
var (
	embedAssetCacheOnce sync.Once
	embedAssetCache     map[string][]byte
)

func getEmbedAssetCache() map[string][]byte {
	embedAssetCacheOnce.Do(func() {
		out := make(map[string][]byte)
		_ = fs.WalkDir(webDistFS, "webdist", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			content, err := webDistFS.ReadFile(path)
			if err != nil {
				return nil
			}
			rel := strings.TrimPrefix(path, "webdist/")
			if rel == "" || rel == ".gitkeep" {
				return nil
			}
			out[rel] = content
			return nil
		})
		embedAssetCache = out
	})
	return embedAssetCache
}

func loadDevAssets() map[string][]byte {
	candidates := []string{
		filepath.Join("..", "web", "dist"),
		filepath.Join("web", "dist"),
	}
	for _, dir := range candidates {
		if a := loadAssetsFromDir(dir); len(a) > 0 {
			return a
		}
	}
	return nil
}

func loadAssetsFromDir(root string) map[string][]byte {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	out := make(map[string][]byte)
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		out[filepath.ToSlash(rel)] = content
		return nil
	})
	if len(out) == 0 {
		return nil
	}
	return out
}
