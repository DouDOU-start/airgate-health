import { jsxs as l, jsx as e, Fragment as L } from "react/jsx-runtime";
import { useState as u, useEffect as N } from "react";
const U = {
  primary: "#3ecfb4",
  primaryHover: "#62dcc4",
  primarySubtle: "rgba(62, 207, 180, 0.08)",
  primaryGlow: "rgba(62, 207, 180, 0.14)",
  success: "#34d399",
  successSubtle: "rgba(52, 211, 153, 0.12)",
  warning: "#fbbf24",
  warningSubtle: "rgba(251, 191, 36, 0.12)",
  danger: "#fb7185",
  dangerSubtle: "rgba(251, 113, 133, 0.12)",
  info: "#7dd3fc",
  infoSubtle: "rgba(125, 211, 252, 0.12)",
  // 背景：深蓝黑，带微蓝底调增加深度感
  bgDeep: "#06080e",
  bg: "#0c0f17",
  bgElevated: "#131722",
  bgSurface: "#1a1e2a",
  bgHover: "#232836",
  bgActive: "#2c3240",
  // 边框：蓝调透明
  border: "rgba(148, 175, 225, 0.08)",
  borderSubtle: "rgba(148, 175, 225, 0.05)",
  borderFocus: "rgba(62, 207, 180, 0.40)",
  // 文字：微蓝白，长时间阅读更舒适
  text: "#e2e6f0",
  textSecondary: "#8d93a8",
  textTertiary: "#565d73",
  textInverse: "#06080e",
  glass: "rgba(148, 175, 225, 0.03)",
  glassBorder: "rgba(148, 175, 225, 0.06)",
  shadowSm: "0 2px 8px rgba(0, 0, 0, 0.36)",
  shadowMd: "0 8px 24px rgba(0, 0, 0, 0.48)",
  shadowLg: "0 20px 48px rgba(0, 0, 0, 0.60)",
  shadowGlow: "0 0 0 1px rgba(62, 207, 180, 0.08), 0 8px 32px rgba(62, 207, 180, 0.10)"
}, Y = {
  radiusSm: "12px",
  radiusMd: "18px",
  radiusLg: "22px",
  radiusXl: "28px",
  fontSans: "'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
  fontMono: "'JetBrains Mono', 'SF Mono', 'Cascadia Code', monospace",
  transition: "200ms cubic-bezier(0.4, 0, 0.2, 1)",
  transitionSlow: "400ms cubic-bezier(0.4, 0, 0.2, 1)"
}, ee = {
  sidebarWidth: "260px",
  sidebarCollapsed: "72px",
  topbarHeight: "64px"
}, A = {
  ...Y,
  ...ee
}, J = {
  dark: U
};
function te(n) {
  return n.replace(/[A-Z]/g, (o) => "-" + o.toLowerCase());
}
function Z(n = "ag") {
  return n.trim() || "ag";
}
function _(n, o) {
  return `--${n}-${te(o)}`;
}
Object.keys(J.dark).reduce((n, o) => (n[o] = _("ag", o), n), {});
Object.keys(A).reduce((n, o) => (n[o] = _("ag", o), n), {});
function q(n = {}) {
  const o = Z(n.prefix);
  return Object.keys(J.dark).reduce((r, s) => (r[s] = _(o, s), r), {});
}
function K(n = {}) {
  const o = Z(n.prefix);
  return Object.keys(A).reduce((r, s) => (r[s] = _(o, s), r), {});
}
const ne = q(), re = K();
function t(n, o = {}) {
  const r = o.prefix ? q(o) : ne, s = o.prefix ? K(o) : re;
  if (n in r) {
    const c = n;
    return `var(${r[c]}, ${U[c]})`;
  }
  const d = n;
  return `var(${s[d]}, ${A[d]})`;
}
const ae = "/api/v1/ext/airgate-health";
async function x(n, o, r) {
  const s = {};
  r !== void 0 && (s["Content-Type"] = "application/json");
  const d = localStorage.getItem("token");
  d && (s.Authorization = `Bearer ${d}`);
  const c = await fetch(ae + o, {
    method: n,
    headers: s,
    body: r ? JSON.stringify(r) : void 0
  }), b = await c.text();
  let g = null;
  try {
    g = b ? JSON.parse(b) : null;
  } catch {
  }
  if (!c.ok) {
    const m = g, S = (m == null ? void 0 : m.message) || (g == null ? void 0 : g.error) || `HTTP ${c.status}`;
    throw c.status === 401 && (localStorage.removeItem("token"), window.location.href = "/login"), new Error(S);
  }
  const p = g;
  if (p && typeof p == "object" && "code" in p && "data" in p) {
    if (p.code !== 0) throw new Error(p.message || "请求失败");
    return p.data;
  }
  return g;
}
const v = {
  overview: (n = "7d") => x("GET", `/admin/overview?window=${n}`),
  accounts: (n = "7d", o = "") => {
    const r = new URLSearchParams({ window: n });
    return o && r.set("platform", o), x(
      "GET",
      `/admin/accounts?${r.toString()}`
    );
  },
  accountDetail: (n, o = "7d") => x("GET", `/admin/accounts/${n}?window=${o}`),
  probe: (n) => x("POST", `/admin/probe/${n}`),
  getMaintenance: () => x("GET", "/admin/maintenance"),
  setMaintenance: (n) => x("PUT", "/admin/maintenance", n),
  getAnnouncement: () => x("GET", "/admin/announcement"),
  setAnnouncement: (n) => x("PUT", "/admin/announcement", n)
};
function oe() {
  const [n, o] = u("7d"), [r, s] = u(null), [d, c] = u([]), [b, g] = u(""), [p, m] = u(!0), [S, f] = u(null), [k, $] = u(null), w = async () => {
    m(!0), f(null);
    try {
      const [i, a] = await Promise.all([
        v.overview(n),
        v.accounts(n, b)
      ]);
      s(i), c(a.list || []);
    } catch (i) {
      f(i instanceof Error ? i.message : String(i));
    } finally {
      m(!1);
    }
  };
  N(() => {
    w();
  }, [n, b]);
  const C = async (i) => {
    $(i);
    try {
      const a = await v.probe(i);
      alert(
        a.Success ? `探测成功，耗时 ${a.LatencyMS}ms` : `探测失败 (${a.ErrorKind}): ${a.ErrorMsg}`
      ), await w();
    } catch (a) {
      alert("探测失败: " + (a instanceof Error ? a.message : String(a)));
    } finally {
      $(null);
    }
  };
  return /* @__PURE__ */ l("div", { style: se, children: [
    /* @__PURE__ */ l("header", { style: { marginBottom: 24 }, children: [
      /* @__PURE__ */ e("h1", { style: ce, children: "健康监控" }),
      /* @__PURE__ */ e("div", { style: de, children: "主动探测各账号连通性，聚合可用率与延迟" })
    ] }),
    (r == null ? void 0 : r.maintenance.enabled) && /* @__PURE__ */ e(R, { level: "warning", text: `维护中：${r.maintenance.message || "系统正在维护"}` }),
    (r == null ? void 0 : r.announcement.enabled) && /* @__PURE__ */ e(
      R,
      {
        level: r.announcement.level === "critical" ? "critical" : "warning",
        text: r.announcement.message
      }
    ),
    /* @__PURE__ */ e(
      ie,
      {
        windowSel: n,
        setWindowSel: o,
        filter: b,
        setFilter: g,
        onReload: w
      }
    ),
    S && /* @__PURE__ */ l("div", { style: ye, children: [
      "错误: ",
      S
    ] }),
    p && !r && /* @__PURE__ */ e("div", { style: j, children: "加载中…" }),
    r && /* @__PURE__ */ l(L, { children: [
      /* @__PURE__ */ e("h2", { style: E, children: "平台总览" }),
      /* @__PURE__ */ e("div", { style: { display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))", gap: 12 }, children: r.platforms.map((i) => /* @__PURE__ */ l("div", { style: ue, children: [
        /* @__PURE__ */ l("div", { style: { display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 10 }, children: [
          /* @__PURE__ */ e("strong", { style: { color: t("text") }, children: i.platform }),
          /* @__PURE__ */ e(W, { color: i.status_color })
        ] }),
        /* @__PURE__ */ e("div", { style: ge, children: i.uptime_pct < 0 ? "—" : i.uptime_pct.toFixed(2) + "%" }),
        /* @__PURE__ */ l("div", { style: pe, children: [
          "p95 ",
          i.latency_p95,
          "ms · ",
          i.account_count,
          " 账号"
        ] })
      ] }, i.platform)) }),
      r.groups.length > 0 && /* @__PURE__ */ l(L, { children: [
        /* @__PURE__ */ e("h2", { style: E, children: "分组聚合" }),
        /* @__PURE__ */ e("div", { style: I, children: /* @__PURE__ */ l("table", { style: P, children: [
          /* @__PURE__ */ e("thead", { children: /* @__PURE__ */ l("tr", { children: [
            /* @__PURE__ */ e("th", { style: h, children: "分组" }),
            /* @__PURE__ */ e("th", { style: h, children: "平台" }),
            /* @__PURE__ */ e("th", { style: h, children: "账号数" }),
            /* @__PURE__ */ e("th", { style: h, children: "可用率" }),
            /* @__PURE__ */ e("th", { style: h, children: "p95 延迟" })
          ] }) }),
          /* @__PURE__ */ e("tbody", { children: r.groups.map((i) => /* @__PURE__ */ l("tr", { children: [
            /* @__PURE__ */ e("td", { style: y, children: i.group_name }),
            /* @__PURE__ */ e("td", { style: y, children: i.platform }),
            /* @__PURE__ */ e("td", { style: y, children: i.account_count }),
            /* @__PURE__ */ l("td", { style: y, children: [
              /* @__PURE__ */ e(W, { color: i.status_color }),
              /* @__PURE__ */ e("span", { style: { marginLeft: 8 }, children: i.uptime_pct < 0 ? "—" : i.uptime_pct.toFixed(2) + "%" })
            ] }),
            /* @__PURE__ */ l("td", { style: y, children: [
              i.latency_p95,
              "ms"
            ] })
          ] }, i.group_id)) })
        ] }) })
      ] }),
      /* @__PURE__ */ e("h2", { style: E, children: "账号明细" }),
      d.length === 0 ? /* @__PURE__ */ e("div", { style: j, children: "暂无账号数据" }) : /* @__PURE__ */ e("div", { style: I, children: /* @__PURE__ */ l("table", { style: P, children: [
        /* @__PURE__ */ e("thead", { children: /* @__PURE__ */ l("tr", { children: [
          /* @__PURE__ */ e("th", { style: h, children: "名称" }),
          /* @__PURE__ */ e("th", { style: h, children: "平台" }),
          /* @__PURE__ */ e("th", { style: h, children: "状态" }),
          /* @__PURE__ */ e("th", { style: h, children: "可用率" }),
          /* @__PURE__ */ e("th", { style: h, children: "p95 延迟" }),
          /* @__PURE__ */ e("th", { style: h, children: "最近探测" }),
          /* @__PURE__ */ e("th", { style: h, children: "操作" })
        ] }) }),
        /* @__PURE__ */ e("tbody", { children: d.map((i) => /* @__PURE__ */ l("tr", { children: [
          /* @__PURE__ */ e("td", { style: y, children: i.account_name }),
          /* @__PURE__ */ e("td", { style: y, children: i.platform }),
          /* @__PURE__ */ e("td", { style: y, children: /* @__PURE__ */ e(le, { status: i.status }) }),
          /* @__PURE__ */ e("td", { style: y, children: i.uptime_pct < 0 ? "—" : i.uptime_pct.toFixed(2) + "%" }),
          /* @__PURE__ */ l("td", { style: y, children: [
            i.latency_p95,
            "ms"
          ] }),
          /* @__PURE__ */ e("td", { style: y, children: i.last_probed_at ? new Date(i.last_probed_at).toLocaleString() : "—" }),
          /* @__PURE__ */ e("td", { style: y, children: /* @__PURE__ */ e(
            "button",
            {
              onClick: () => C(i.account_id),
              disabled: k === i.account_id,
              style: X,
              children: k === i.account_id ? "探测中…" : "立即探测"
            }
          ) })
        ] }, i.account_id)) })
      ] }) })
    ] })
  ] });
}
function ie({
  windowSel: n,
  setWindowSel: o,
  filter: r,
  setFilter: s,
  onReload: d
}) {
  return /* @__PURE__ */ l("div", { style: { display: "flex", gap: 12, alignItems: "center", margin: "0 0 20px" }, children: [
    /* @__PURE__ */ l("select", { value: n, onChange: (c) => o(c.target.value), style: he, children: [
      /* @__PURE__ */ e("option", { value: "7d", children: "最近 7 天" }),
      /* @__PURE__ */ e("option", { value: "15d", children: "最近 15 天" }),
      /* @__PURE__ */ e("option", { value: "30d", children: "最近 30 天" })
    ] }),
    /* @__PURE__ */ e(
      "input",
      {
        type: "text",
        placeholder: "按平台过滤（留空显示全部）",
        value: r,
        onChange: (c) => s(c.target.value),
        style: { ...Q, flex: 1 }
      }
    ),
    /* @__PURE__ */ e("button", { onClick: d, style: X, children: "刷新" })
  ] });
}
function R({ level: n, text: o }) {
  const r = t(n === "critical" ? "dangerSubtle" : "warningSubtle"), s = t(n === "critical" ? "danger" : "warning"), d = t(n === "critical" ? "danger" : "warning");
  return /* @__PURE__ */ e(
    "div",
    {
      style: {
        background: r,
        color: s,
        borderLeft: `3px solid ${d}`,
        padding: "12px 16px",
        borderRadius: t("radiusMd"),
        marginBottom: 16,
        fontSize: 13
      },
      children: o
    }
  );
}
function W({ color: n }) {
  const o = {
    green: t("success"),
    yellow: t("warning"),
    red: t("danger"),
    gray: t("textTertiary")
  };
  return /* @__PURE__ */ e(
    "span",
    {
      style: {
        display: "inline-block",
        width: 10,
        height: 10,
        borderRadius: "50%",
        background: o[n],
        boxShadow: `0 0 0 2px ${t("bgSurface")}`,
        verticalAlign: "middle"
      }
    }
  );
}
function le({ status: n }) {
  const o = {
    active: { bg: t("successSubtle"), fg: t("success"), label: "正常" },
    error: { bg: t("dangerSubtle"), fg: t("danger"), label: "故障" },
    disabled: { bg: t("bgHover"), fg: t("textSecondary"), label: "禁用" }
  }, r = o[n] || o.disabled;
  return /* @__PURE__ */ e(
    "span",
    {
      style: {
        background: r.bg,
        color: r.fg,
        padding: "2px 10px",
        borderRadius: t("radiusSm"),
        fontSize: 12,
        fontWeight: 500
      },
      children: r.label
    }
  );
}
const se = {
  maxWidth: 1200,
  margin: "0 auto",
  padding: "24px 24px 48px",
  color: t("text")
}, ce = {
  margin: "0 0 6px",
  fontSize: 24,
  fontWeight: 600,
  color: t("text"),
  letterSpacing: "-0.01em"
}, de = {
  color: t("textSecondary"),
  fontSize: 13
}, E = {
  margin: "28px 0 12px",
  fontSize: 13,
  fontWeight: 600,
  color: t("textSecondary"),
  textTransform: "uppercase",
  letterSpacing: "0.04em"
}, ue = {
  background: t("bgSurface"),
  border: `1px solid ${t("glassBorder")}`,
  borderRadius: t("radiusLg"),
  padding: 16
}, ge = {
  fontSize: 26,
  fontWeight: 600,
  fontVariantNumeric: "tabular-nums",
  color: t("text"),
  letterSpacing: "-0.02em"
}, pe = {
  fontSize: 12,
  color: t("textSecondary"),
  marginTop: 6
}, I = {
  background: t("bgSurface"),
  border: `1px solid ${t("glassBorder")}`,
  borderRadius: t("radiusLg"),
  overflow: "hidden"
}, P = {
  width: "100%",
  borderCollapse: "collapse"
}, h = {
  textAlign: "left",
  padding: "12px 16px",
  background: t("bg"),
  fontSize: 11,
  fontWeight: 600,
  color: t("textSecondary"),
  textTransform: "uppercase",
  letterSpacing: "0.04em",
  borderBottom: `1px solid ${t("glassBorder")}`
}, y = {
  padding: "12px 16px",
  borderBottom: `1px solid ${t("borderSubtle")}`,
  fontSize: 13,
  color: t("text")
}, X = {
  padding: "8px 16px",
  background: t("primary"),
  color: t("textInverse"),
  border: "none",
  borderRadius: t("radiusMd"),
  cursor: "pointer",
  fontSize: 12,
  fontWeight: 600,
  transition: t("transition")
}, Q = {
  padding: "9px 12px",
  border: `1px solid ${t("glassBorder")}`,
  borderRadius: t("radiusMd"),
  background: t("bg"),
  color: t("text"),
  fontSize: 13,
  outline: "none"
}, he = {
  ...Q,
  cursor: "pointer"
}, ye = {
  background: t("dangerSubtle"),
  color: t("danger"),
  borderLeft: `3px solid ${t("danger")}`,
  padding: "12px 16px",
  borderRadius: t("radiusMd"),
  marginBottom: 12,
  fontSize: 13
}, j = {
  color: t("textTertiary"),
  padding: "32px 0",
  textAlign: "center",
  fontSize: 13
};
function be() {
  const [n, o] = u({ enabled: !1, message: "", platforms: [] }), [r, s] = u({ enabled: !1, level: "info", message: "" }), [d, c] = u(!0), [b, g] = u(!1), [p, m] = u(!1), [S, f] = u(null), [k, $] = u(null);
  if (N(() => {
    Promise.all([v.getMaintenance(), v.getAnnouncement()]).then(([a, z]) => {
      o(a), s(z);
    }).catch((a) => f(a instanceof Error ? a.message : String(a))).finally(() => c(!1));
  }, []), d) return /* @__PURE__ */ e("div", { style: O, children: /* @__PURE__ */ e("div", { style: ve, children: "加载中…" }) });
  const w = (a) => {
    $(a), setTimeout(() => $(null), 2400);
  }, C = async () => {
    g(!0), f(null);
    try {
      const a = await v.setMaintenance(n);
      o(a), w("维护设置已保存");
    } catch (a) {
      f(a instanceof Error ? a.message : String(a));
    } finally {
      g(!1);
    }
  }, i = async () => {
    m(!0), f(null);
    try {
      const a = await v.setAnnouncement(r);
      s(a), w("公告已保存");
    } catch (a) {
      f(a instanceof Error ? a.message : String(a));
    } finally {
      m(!1);
    }
  };
  return /* @__PURE__ */ l("div", { style: O, children: [
    /* @__PURE__ */ l("header", { style: { marginBottom: 24 }, children: [
      /* @__PURE__ */ e("h1", { style: me, children: "维护与公告" }),
      /* @__PURE__ */ e("div", { style: fe, children: "管理维护模式与对外状态页的公告横幅" })
    ] }),
    S && /* @__PURE__ */ l("div", { style: xe, children: [
      "错误: ",
      S
    ] }),
    k && /* @__PURE__ */ e("div", { style: Se, children: k }),
    /* @__PURE__ */ l("section", { style: F, children: [
      /* @__PURE__ */ e("h2", { style: V, children: "维护模式" }),
      /* @__PURE__ */ l("label", { style: G, children: [
        /* @__PURE__ */ e(
          "input",
          {
            type: "checkbox",
            checked: n.enabled,
            onChange: (a) => o({ ...n, enabled: a.target.checked }),
            style: D
          }
        ),
        /* @__PURE__ */ e("span", { children: "启用维护模式" })
      ] }),
      /* @__PURE__ */ l("div", { style: M, children: [
        /* @__PURE__ */ e("label", { style: T, children: "提示信息" }),
        /* @__PURE__ */ e(
          "input",
          {
            type: "text",
            value: n.message || "",
            onChange: (a) => o({ ...n, message: a.target.value }),
            placeholder: "例：系统升级中，预计 10 分钟后恢复",
            style: B
          }
        )
      ] }),
      /* @__PURE__ */ l("div", { style: M, children: [
        /* @__PURE__ */ e("label", { style: T, children: "受影响平台（逗号分隔，留空表示全局维护）" }),
        /* @__PURE__ */ e(
          "input",
          {
            type: "text",
            value: (n.platforms || []).join(","),
            onChange: (a) => o({
              ...n,
              platforms: a.target.value.split(",").map((z) => z.trim()).filter(Boolean)
            }),
            placeholder: "例：openai,anthropic",
            style: B
          }
        )
      ] }),
      /* @__PURE__ */ e("button", { onClick: C, disabled: b, style: H, children: b ? "保存中…" : "保存维护设置" })
    ] }),
    /* @__PURE__ */ l("section", { style: F, children: [
      /* @__PURE__ */ e("h2", { style: V, children: "公告横幅" }),
      /* @__PURE__ */ l("label", { style: G, children: [
        /* @__PURE__ */ e(
          "input",
          {
            type: "checkbox",
            checked: r.enabled,
            onChange: (a) => s({ ...r, enabled: a.target.checked }),
            style: D
          }
        ),
        /* @__PURE__ */ e("span", { children: "启用公告" })
      ] }),
      /* @__PURE__ */ l("div", { style: M, children: [
        /* @__PURE__ */ e("label", { style: T, children: "级别" }),
        /* @__PURE__ */ l(
          "select",
          {
            value: r.level,
            onChange: (a) => s({ ...r, level: a.target.value }),
            style: { ...B, cursor: "pointer" },
            children: [
              /* @__PURE__ */ e("option", { value: "info", children: "info（信息）" }),
              /* @__PURE__ */ e("option", { value: "warning", children: "warning（警告）" }),
              /* @__PURE__ */ e("option", { value: "critical", children: "critical（严重）" })
            ]
          }
        )
      ] }),
      /* @__PURE__ */ l("div", { style: M, children: [
        /* @__PURE__ */ e("label", { style: T, children: "消息内容" }),
        /* @__PURE__ */ e(
          "input",
          {
            type: "text",
            value: r.message || "",
            onChange: (a) => s({ ...r, message: a.target.value }),
            placeholder: "将显示在状态页顶部",
            style: B
          }
        )
      ] }),
      /* @__PURE__ */ l("div", { style: M, children: [
        /* @__PURE__ */ e("label", { style: T, children: "过期时间（ISO 8601；留空表示永久）" }),
        /* @__PURE__ */ e(
          "input",
          {
            type: "text",
            value: r.expires_at && r.expires_at !== "0001-01-01T00:00:00Z" ? r.expires_at : "",
            onChange: (a) => s({ ...r, expires_at: a.target.value }),
            placeholder: "2026-12-31T23:59:59Z",
            style: B
          }
        )
      ] }),
      /* @__PURE__ */ e("button", { onClick: i, disabled: p, style: H, children: p ? "保存中…" : "保存公告" })
    ] })
  ] });
}
const O = {
  maxWidth: 800,
  margin: "0 auto",
  padding: "24px 24px 48px",
  color: t("text")
}, me = {
  margin: "0 0 6px",
  fontSize: 24,
  fontWeight: 600,
  color: t("text"),
  letterSpacing: "-0.01em"
}, fe = {
  color: t("textSecondary"),
  fontSize: 13
}, F = {
  background: t("bgSurface"),
  border: `1px solid ${t("glassBorder")}`,
  borderRadius: t("radiusLg"),
  padding: 24,
  marginBottom: 16
}, V = {
  margin: "0 0 18px",
  fontSize: 13,
  fontWeight: 600,
  color: t("textSecondary"),
  textTransform: "uppercase",
  letterSpacing: "0.04em"
}, G = {
  display: "flex",
  gap: 10,
  alignItems: "center",
  marginBottom: 18,
  cursor: "pointer",
  color: t("text"),
  fontSize: 14
}, D = {
  width: 16,
  height: 16,
  cursor: "pointer",
  accentColor: "#3ecfb4"
  // hardcoded primary fallback for native checkbox
}, M = {
  marginBottom: 14
}, T = {
  display: "block",
  fontSize: 12,
  color: t("textSecondary"),
  marginBottom: 6
}, B = {
  width: "100%",
  padding: "10px 14px",
  border: `1px solid ${t("glassBorder")}`,
  borderRadius: t("radiusMd"),
  background: t("bg"),
  color: t("text"),
  fontSize: 13,
  outline: "none",
  boxSizing: "border-box",
  transition: t("transition")
}, H = {
  padding: "10px 20px",
  background: t("primary"),
  color: t("textInverse"),
  border: "none",
  borderRadius: t("radiusMd"),
  cursor: "pointer",
  fontSize: 13,
  fontWeight: 600,
  transition: t("transition"),
  marginTop: 4
}, xe = {
  background: t("dangerSubtle"),
  color: t("danger"),
  borderLeft: `3px solid ${t("danger")}`,
  padding: "12px 16px",
  borderRadius: t("radiusMd"),
  marginBottom: 12,
  fontSize: 13
}, Se = {
  background: t("successSubtle"),
  color: t("success"),
  borderLeft: `3px solid ${t("success")}`,
  padding: "12px 16px",
  borderRadius: t("radiusMd"),
  marginBottom: 12,
  fontSize: 13
}, ve = {
  color: t("textTertiary"),
  padding: "32px 0",
  textAlign: "center",
  fontSize: 13
}, $e = {
  routes: [
    { path: "/admin/health", component: oe },
    { path: "/admin/health-maintenance", component: be }
  ]
};
export {
  $e as default
};
