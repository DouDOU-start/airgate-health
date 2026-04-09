package health

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"

	sdk "github.com/DouDOU-start/airgate-sdk"
)

// HTTP header（与 core/extension_proxy.go 注入的头一致）
const (
	headerEntry = "X-Airgate-Entry" // admin / user / callback / public
	headerRole  = "X-Airgate-Role"  // admin / user
)

// registerRoutes 把所有 handler 挂到 sdk.RouteRegistrar 上。
//
// 路径分布（注意：core 的 ExtensionProxy 把 :pluginName 与 /status 前缀都剥掉，
// 所以插件这边看到的 path 已经不带前缀）：
//
//	admin 入口   (/api/v1/ext/airgate-health/admin/xxx → 插件看到 /admin/xxx)
//	  GET    /admin/overview                    平台 + 账号 总览
//	  GET    /admin/accounts                    账号摘要列表（?platform=&window=）
//	  GET    /admin/accounts/{id}               单账号详情 + 90 天日桶
//	  GET    /admin/groups                      group 维度聚合
//	  POST   /admin/probe/group/{id}            手动触发一次整组探测
//
//	public 入口  (/status, /status/api/summary, /status/assets/* → 插件看到 /, /api/summary, /assets/*)
//	  GET    /                                  静态 HTML 入口（status.html）
//	  GET    /api/summary                       脱敏的分组维度聚合（含备注、90 天方格图）
//	  GET    /assets/                           静态资源前缀
func (p *Plugin) registerRoutes(r sdk.RouteRegistrar) {
	// === admin ===
	r.Handle(http.MethodGet, "/admin/overview", p.requireAdmin(p.requireConfigured(p.handleOverview)))
	r.Handle(http.MethodGet, "/admin/accounts", p.requireAdmin(p.requireConfigured(p.handleAdminAccounts)))
	r.Handle(http.MethodGet, "/admin/accounts/", p.requireAdmin(p.requireConfigured(p.handleAdminAccountDetail))) // 前缀匹配 /admin/accounts/{id}
	r.Handle(http.MethodGet, "/admin/groups", p.requireAdmin(p.requireConfigured(p.handleAdminGroups)))
	r.Handle(http.MethodPost, "/admin/probe/group/", p.requireAdmin(p.requireConfigured(p.handleAdminProbeGroup))) // /admin/probe/group/{group_id}

	// === public（通过 core /status/* 反向代理；core 已剥掉 /status 前缀）===
	r.Handle(http.MethodGet, "/api/summary", p.requirePublic(p.requireConfigured(p.handlePublicSummary)))
	r.Handle(http.MethodGet, "/", p.requirePublic(p.handlePublicIndex))
	r.Handle(http.MethodGet, "/assets/", p.requirePublic(p.handlePublicAsset)) // /assets/* 前缀匹配
}

// ============================================================================
// 入口校验中间件
// ============================================================================

func (p *Plugin) requireConfigured(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !p.Configured() {
			writeJSONErr(w, http.StatusServiceUnavailable,
				"健康监控插件尚未就绪：请管理员在「系统设置 → 安全与认证」生成 admin API key，然后在「插件管理」热加载本插件")
			return
		}
		next(w, r)
	}
}

func (p *Plugin) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerEntry) != "admin" {
			writeJSONErr(w, http.StatusForbidden, "该接口仅允许通过 /api/v1/ext 入口访问")
			return
		}
		if r.Header.Get(headerRole) != "admin" {
			writeJSONErr(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next(w, r)
	}
}

// requirePublic 仅允许 X-Airgate-Entry=public（由 core router 注入）。
// 如果公开开关被关闭，直接 404 模拟"路径不存在"。
func (p *Plugin) requirePublic(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(headerEntry) != "public" {
			writeJSONErr(w, http.StatusForbidden, "公开状态页仅允许通过 /status 入口访问")
			return
		}
		if !p.publicEnabled.Load() {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

// ============================================================================
// admin handlers
// ============================================================================

func (p *Plugin) handleOverview(w http.ResponseWriter, r *http.Request) {
	w0 := ParseWindow(r.URL.Query().Get("window"))
	platforms, err := p.agg.PlatformHealthList(r.Context(), w0, false)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	groups, err := p.agg.GroupHealthList(r.Context(), w0, false)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 保证空切片序列化为 JSON [] 而不是 null，避免前端 .map 崩溃
	if platforms == nil {
		platforms = []PlatformHealth{}
	}
	if groups == nil {
		groups = []GroupHealth{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window":    w0.Name,
		"platforms": platforms,
		"groups":    groups,
	})
}

func (p *Plugin) handleAdminAccounts(w http.ResponseWriter, r *http.Request) {
	w0 := ParseWindow(r.URL.Query().Get("window"))
	platform := r.URL.Query().Get("platform")
	list, err := p.agg.AccountSummariesByPlatform(r.Context(), platform, w0)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []AccountSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window": w0.Name,
		"list":   list,
	})
}

func (p *Plugin) handleAdminAccountDetail(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/admin/accounts/")
	idStr = strings.TrimSuffix(idStr, "/")
	id, err := ParseAccountID(idStr)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w0 := ParseWindow(r.URL.Query().Get("window"))
	detail, err := p.agg.AccountHealthByID(r.Context(), id, w0)
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (p *Plugin) handleAdminGroups(w http.ResponseWriter, r *http.Request) {
	w0 := ParseWindow(r.URL.Query().Get("window"))
	list, err := p.agg.GroupHealthList(r.Context(), w0, false)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []GroupHealth{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window": w0.Name,
		"list":   list,
	})
}

func (p *Plugin) handleAdminProbeGroup(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/admin/probe/group/")
	idStr = strings.TrimSuffix(idStr, "/")
	id, err := ParseAccountID(idStr) // 复用：解析正整数
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "无效的分组 ID")
		return
	}
	res, err := p.prober.ProbeGroup(r.Context(), id)
	if err != nil {
		var notFound *GroupNotFoundError
		if errors.As(err, &notFound) {
			writeJSONErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ============================================================================
// public handlers（脱敏：不含 account_id 与 error_msg）
// ============================================================================

// handlePublicSummary 公开状态页的数据接口。
//
// 脱敏规则：
//   - 只暴露 group 维度（group_name + platform + 状态 + 备注 + 90 天方格图），
//     让订阅了具体分组的用户能识别自己使用的渠道是否受影响。
//   - 备注本身就是面向用户的运维说明（管理员在「分组管理」页填写）。
//   - 不返回 account_id / account name / error_msg。
//   - 旧的"按 platform 维度聚合"已下线：平台维度对终端用户无意义（用户订阅的是
//     具体分组而不是 platform），且与分组列表内容重复。
func (p *Plugin) handlePublicSummary(w http.ResponseWriter, r *http.Request) {
	w0 := ParseWindow(r.URL.Query().Get("window"))
	groups, err := p.agg.GroupHealthList(r.Context(), w0, true)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "服务暂不可用")
		return
	}
	if groups == nil {
		groups = []GroupHealth{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window": w0.Name,
		"groups": groups,
	})
}

// handlePublicIndex 返回 webdist/status.html（独立打包的状态页 HTML）。
// 如果 webdist 没有 status.html，返回最小化的内嵌 HTML 占位。
func (p *Plugin) handlePublicIndex(w http.ResponseWriter, _ *http.Request) {
	if data, ok := p.readAsset("status.html"); ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(fallbackStatusHTML))
}

// handlePublicAsset serves /assets/* 静态资源（core 已经剥掉了 /status 前缀）。
func (p *Plugin) handlePublicAsset(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/")
	if rel == "" {
		p.handlePublicIndex(w, r)
		return
	}
	// 简单的路径穿越防护
	clean := path.Clean("/" + rel)
	if strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}
	rel = strings.TrimPrefix(clean, "/")
	data, ok := p.readAsset(rel)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentTypeFromExt(rel))
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(data)
}

// fallbackStatusHTML 当插件 webdist 还没有 status.html 时返回的最小占位页。
// 包含一段 inline JS 调用 /status/api/summary 渲染最基础的卡片。
const fallbackStatusHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<title>服务状态</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  body { font-family: -apple-system, sans-serif; max-width: 800px; margin: 40px auto; padding: 0 16px; color: #1f2937; background: #f9fafb; }
  h1 { font-size: 24px; margin-bottom: 8px; }
  .card { background: white; padding: 16px; border-radius: 8px; margin: 12px 0; box-shadow: 0 1px 2px rgba(0,0,0,0.05); }
  .row { display: flex; justify-content: space-between; align-items: center; }
  .name { font-weight: 600; }
  .pct { font-variant-numeric: tabular-nums; }
  .note { margin-top: 4px; margin-left: 18px; color: #6b7280; font-size: 13px; white-space: pre-wrap; }
  .dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-right: 8px; vertical-align: middle; }
  .green { background: #10b981; } .yellow { background: #f59e0b; } .red { background: #ef4444; } .gray { background: #9ca3af; }
  .meta { color: #6b7280; font-size: 13px; }
</style>
</head><body>
<h1>服务状态</h1>
<div class="meta">最近 7 天可用率（实时刷新）</div>
<div id="root">加载中…</div>
<script>
fetch('/status/api/summary?window=7d').then(r=>r.json()).then(d=>{
  var root = document.getElementById('root');
  root.innerHTML = '';
  function esc(s) { var p = document.createElement('p'); p.textContent = s == null ? '' : String(s); return p.innerHTML; }
  (d.groups || []).forEach(function(g) {
    var c = document.createElement('div'); c.className = 'card';
    var pct = g.uptime_pct < 0 ? '--' : g.uptime_pct.toFixed(2) + '%';
    c.innerHTML = '<div class="row"><span><span class="dot ' + (g.status_color||'gray') + '"></span><span class="name">' + esc(g.group_name) + '</span> <span class="meta">· ' + esc(g.platform) + '</span></span><span class="pct">' + pct + '</span></div>';
    if (g.note) {
      var n = document.createElement('div');
      n.className = 'note';
      n.textContent = g.note;
      c.appendChild(n);
    }
    root.appendChild(c);
  });
  if (!(d.groups||[]).length) {
    root.innerHTML = '<div class="meta">暂无监控数据</div>';
  }
}).catch(function(e){
  document.getElementById('root').textContent = '加载失败: ' + e.message;
});
</script>
</body></html>`

// ============================================================================
// 工具函数
// ============================================================================

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func contentTypeFromExt(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}
