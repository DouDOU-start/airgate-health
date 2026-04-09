import { jsxs as i, jsx as e, Fragment as z } from "react/jsx-runtime";
import { useState as b, useEffect as D } from "react";
const A = {
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
}, H = {
  radiusSm: "12px",
  radiusMd: "18px",
  radiusLg: "22px",
  radiusXl: "28px",
  fontSans: "'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
  fontMono: "'JetBrains Mono', 'SF Mono', 'Cascadia Code', monospace",
  transition: "200ms cubic-bezier(0.4, 0, 0.2, 1)",
  transitionSlow: "400ms cubic-bezier(0.4, 0, 0.2, 1)"
}, q = {
  sidebarWidth: "260px",
  sidebarCollapsed: "72px",
  topbarHeight: "64px"
}, k = {
  ...H,
  ...q
}, R = {
  dark: A
};
function J(n) {
  return n.replace(/[A-Z]/g, (o) => "-" + o.toLowerCase());
}
function j(n = "ag") {
  return n.trim() || "ag";
}
function v(n, o) {
  return `--${n}-${J(o)}`;
}
Object.keys(R.dark).reduce((n, o) => (n[o] = v("ag", o), n), {});
Object.keys(k).reduce((n, o) => (n[o] = v("ag", o), n), {});
function F(n = {}) {
  const o = j(n.prefix);
  return Object.keys(R.dark).reduce((a, s) => (a[s] = v(o, s), a), {});
}
function I(n = {}) {
  const o = j(n.prefix);
  return Object.keys(k).reduce((a, s) => (a[s] = v(o, s), a), {});
}
const N = F(), U = I();
function t(n, o = {}) {
  const a = o.prefix ? F(o) : N, s = o.prefix ? I(o) : U;
  if (n in a) {
    const c = n;
    return `var(${a[c]}, ${A[c]})`;
  }
  const l = n;
  return `var(${s[l]}, ${k[l]})`;
}
const K = "/api/v1/ext/airgate-health";
async function S(n, o, a) {
  const s = {}, l = localStorage.getItem("token");
  l && (s.Authorization = `Bearer ${l}`);
  const c = await fetch(K + o, {
    method: n,
    headers: s,
    body: void 0
  }), y = await c.text();
  let g = null;
  try {
    g = y ? JSON.parse(y) : null;
  } catch {
  }
  if (!c.ok) {
    const f = g, x = (f == null ? void 0 : f.message) || (g == null ? void 0 : g.error) || `HTTP ${c.status}`;
    throw c.status === 401 && (localStorage.removeItem("token"), window.location.href = "/login"), new Error(x);
  }
  const h = g;
  if (h && typeof h == "object" && "code" in h && "data" in h) {
    if (h.code !== 0) throw new Error(h.message || "请求失败");
    return h.data;
  }
  return g;
}
const _ = {
  overview: (n = "7d") => S("GET", `/admin/overview?window=${n}`),
  accounts: (n = "7d", o = "") => {
    const a = new URLSearchParams({ window: n });
    return o && a.set("platform", o), S(
      "GET",
      `/admin/accounts?${a.toString()}`
    );
  },
  accountDetail: (n, o = "7d") => S("GET", `/admin/accounts/${n}?window=${o}`),
  probeGroup: (n) => S("POST", `/admin/probe/group/${n}`)
};
function w(n, o, a) {
  var l;
  const s = window;
  if ((l = s.airgate) != null && l.toast) {
    s.airgate.toast(n, o, a);
    return;
  }
  alert(o);
}
function X() {
  const [n, o] = b("7d"), [a, s] = b(null), [l, c] = b([]), [y, g] = b(""), [h, f] = b(!0), [x, B] = b(null), [C, M] = b(null), $ = async () => {
    f(!0), B(null);
    try {
      const [r, m] = await Promise.all([
        _.overview(n),
        _.accounts(n, y)
      ]);
      s({
        ...r,
        platforms: r.platforms ?? [],
        groups: r.groups ?? []
      }), c(m.list || []);
    } catch (r) {
      B(r instanceof Error ? r.message : String(r));
    } finally {
      f(!1);
    }
  };
  D(() => {
    $();
  }, [n, y]);
  const P = async (r, m) => {
    M(r);
    try {
      const p = await _.probeGroup(r);
      p.total === 0 ? w("warning", `分组「${m}」下没有可探测的账号`) : p.failed === 0 ? w(
        "success",
        `分组「${m}」探测完成：${p.success}/${p.total} 成功，耗时 ${p.duration_ms}ms`
      ) : w(
        "warning",
        `分组「${m}」探测完成：${p.success}/${p.total} 成功，${p.failed} 失败`
      ), await $();
    } catch (p) {
      w("error", "探测失败: " + (p instanceof Error ? p.message : String(p)));
    } finally {
      M(null);
    }
  };
  return /* @__PURE__ */ i("div", { style: Y, children: [
    /* @__PURE__ */ i("header", { style: { marginBottom: 24 }, children: [
      /* @__PURE__ */ e("h1", { style: ee, children: "健康监控" }),
      /* @__PURE__ */ e("div", { style: te, children: "主动探测各账号连通性，聚合可用率与延迟" })
    ] }),
    /* @__PURE__ */ e(
      Z,
      {
        windowSel: n,
        setWindowSel: o,
        filter: y,
        setFilter: g,
        onReload: $
      }
    ),
    x && /* @__PURE__ */ i("div", { style: ie, children: [
      "错误: ",
      x
    ] }),
    h && !a && /* @__PURE__ */ e("div", { style: L, children: "加载中…" }),
    a && /* @__PURE__ */ i(z, { children: [
      /* @__PURE__ */ e("h2", { style: T, children: "平台总览" }),
      /* @__PURE__ */ e("div", { style: { display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))", gap: 12 }, children: a.platforms.map((r) => /* @__PURE__ */ i("div", { style: re, children: [
        /* @__PURE__ */ i("div", { style: { display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 10 }, children: [
          /* @__PURE__ */ e("strong", { style: { color: t("text") }, children: r.platform }),
          /* @__PURE__ */ e(E, { color: r.status_color })
        ] }),
        /* @__PURE__ */ e("div", { style: ne, children: r.uptime_pct < 0 ? "—" : r.uptime_pct.toFixed(2) + "%" }),
        /* @__PURE__ */ i("div", { style: oe, children: [
          "p95 ",
          r.latency_p95,
          "ms · ",
          r.account_count,
          " 账号"
        ] })
      ] }, r.platform)) }),
      a.groups.length > 0 && /* @__PURE__ */ i(z, { children: [
        /* @__PURE__ */ e("h2", { style: T, children: "分组聚合" }),
        /* @__PURE__ */ e("div", { style: W, children: /* @__PURE__ */ i("table", { style: G, children: [
          /* @__PURE__ */ e("thead", { children: /* @__PURE__ */ i("tr", { children: [
            /* @__PURE__ */ e("th", { style: d, children: "分组" }),
            /* @__PURE__ */ e("th", { style: d, children: "平台" }),
            /* @__PURE__ */ e("th", { style: d, children: "账号数" }),
            /* @__PURE__ */ e("th", { style: d, children: "可用率" }),
            /* @__PURE__ */ e("th", { style: d, children: "p95 延迟" }),
            /* @__PURE__ */ e("th", { style: d, children: "备注" }),
            /* @__PURE__ */ e("th", { style: d, children: "操作" })
          ] }) }),
          /* @__PURE__ */ e("tbody", { children: a.groups.map((r) => /* @__PURE__ */ i("tr", { children: [
            /* @__PURE__ */ e("td", { style: u, children: r.group_name }),
            /* @__PURE__ */ e("td", { style: u, children: r.platform }),
            /* @__PURE__ */ e("td", { style: u, children: r.account_count }),
            /* @__PURE__ */ i("td", { style: u, children: [
              /* @__PURE__ */ e(E, { color: r.status_color }),
              /* @__PURE__ */ e("span", { style: { marginLeft: 8 }, children: r.uptime_pct < 0 ? "—" : r.uptime_pct.toFixed(2) + "%" })
            ] }),
            /* @__PURE__ */ i("td", { style: u, children: [
              r.latency_p95,
              "ms"
            ] }),
            /* @__PURE__ */ e("td", { style: { ...u, color: r.note ? t("text") : t("textTertiary"), maxWidth: 320, whiteSpace: "pre-wrap" }, children: r.note || "—" }),
            /* @__PURE__ */ e("td", { style: u, children: /* @__PURE__ */ e(
              "button",
              {
                onClick: () => P(r.group_id, r.group_name),
                disabled: C === r.group_id,
                style: V,
                children: C === r.group_id ? "探测中…" : "立即探测"
              }
            ) })
          ] }, r.group_id)) })
        ] }) })
      ] }),
      /* @__PURE__ */ e("h2", { style: T, children: "账号明细" }),
      l.length === 0 ? /* @__PURE__ */ e("div", { style: L, children: "暂无账号数据" }) : /* @__PURE__ */ e("div", { style: W, children: /* @__PURE__ */ i("table", { style: G, children: [
        /* @__PURE__ */ e("thead", { children: /* @__PURE__ */ i("tr", { children: [
          /* @__PURE__ */ e("th", { style: d, children: "名称" }),
          /* @__PURE__ */ e("th", { style: d, children: "平台" }),
          /* @__PURE__ */ e("th", { style: d, children: "状态" }),
          /* @__PURE__ */ e("th", { style: d, children: "可用率" }),
          /* @__PURE__ */ e("th", { style: d, children: "p95 延迟" }),
          /* @__PURE__ */ e("th", { style: d, children: "最近探测" })
        ] }) }),
        /* @__PURE__ */ e("tbody", { children: l.map((r) => /* @__PURE__ */ i("tr", { children: [
          /* @__PURE__ */ e("td", { style: u, children: r.account_name }),
          /* @__PURE__ */ e("td", { style: u, children: r.platform }),
          /* @__PURE__ */ e("td", { style: u, children: /* @__PURE__ */ e(Q, { status: r.status }) }),
          /* @__PURE__ */ e("td", { style: u, children: r.uptime_pct < 0 ? "—" : r.uptime_pct.toFixed(2) + "%" }),
          /* @__PURE__ */ i("td", { style: u, children: [
            r.latency_p95,
            "ms"
          ] }),
          /* @__PURE__ */ e("td", { style: u, children: r.last_probed_at ? new Date(r.last_probed_at).toLocaleString() : "—" })
        ] }, r.account_id)) })
      ] }) })
    ] })
  ] });
}
function Z({
  windowSel: n,
  setWindowSel: o,
  filter: a,
  setFilter: s,
  onReload: l
}) {
  return /* @__PURE__ */ i("div", { style: { display: "flex", gap: 12, alignItems: "center", margin: "0 0 20px" }, children: [
    /* @__PURE__ */ i("select", { value: n, onChange: (c) => o(c.target.value), style: ae, children: [
      /* @__PURE__ */ e("option", { value: "7d", children: "最近 7 天" }),
      /* @__PURE__ */ e("option", { value: "15d", children: "最近 15 天" }),
      /* @__PURE__ */ e("option", { value: "30d", children: "最近 30 天" })
    ] }),
    /* @__PURE__ */ e(
      "input",
      {
        type: "text",
        placeholder: "按平台过滤（留空显示全部）",
        value: a,
        onChange: (c) => s(c.target.value),
        style: { ...O, flex: 1 }
      }
    ),
    /* @__PURE__ */ e("button", { onClick: l, style: V, children: "刷新" })
  ] });
}
function E({ color: n }) {
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
function Q({ status: n }) {
  const o = {
    active: { bg: t("successSubtle"), fg: t("success"), label: "正常" },
    error: { bg: t("dangerSubtle"), fg: t("danger"), label: "故障" },
    disabled: { bg: t("bgHover"), fg: t("textSecondary"), label: "禁用" }
  }, a = o[n] || o.disabled;
  return /* @__PURE__ */ e(
    "span",
    {
      style: {
        background: a.bg,
        color: a.fg,
        padding: "2px 10px",
        borderRadius: t("radiusSm"),
        fontSize: 12,
        fontWeight: 500
      },
      children: a.label
    }
  );
}
const Y = {
  maxWidth: 1200,
  margin: "0 auto",
  padding: "24px 24px 48px",
  color: t("text")
}, ee = {
  margin: "0 0 6px",
  fontSize: 24,
  fontWeight: 600,
  color: t("text"),
  letterSpacing: "-0.01em"
}, te = {
  color: t("textSecondary"),
  fontSize: 13
}, T = {
  margin: "28px 0 12px",
  fontSize: 13,
  fontWeight: 600,
  color: t("textSecondary"),
  textTransform: "uppercase",
  letterSpacing: "0.04em"
}, re = {
  background: t("bgSurface"),
  border: `1px solid ${t("glassBorder")}`,
  borderRadius: t("radiusLg"),
  padding: 16
}, ne = {
  fontSize: 26,
  fontWeight: 600,
  fontVariantNumeric: "tabular-nums",
  color: t("text"),
  letterSpacing: "-0.02em"
}, oe = {
  fontSize: 12,
  color: t("textSecondary"),
  marginTop: 6
}, W = {
  background: t("bgSurface"),
  border: `1px solid ${t("glassBorder")}`,
  borderRadius: t("radiusLg"),
  overflow: "hidden"
}, G = {
  width: "100%",
  borderCollapse: "collapse"
}, d = {
  textAlign: "left",
  padding: "12px 16px",
  background: t("bg"),
  fontSize: 11,
  fontWeight: 600,
  color: t("textSecondary"),
  textTransform: "uppercase",
  letterSpacing: "0.04em",
  borderBottom: `1px solid ${t("glassBorder")}`
}, u = {
  padding: "12px 16px",
  borderBottom: `1px solid ${t("borderSubtle")}`,
  fontSize: 13,
  color: t("text")
}, V = {
  padding: "8px 16px",
  background: t("primary"),
  color: t("textInverse"),
  border: "none",
  borderRadius: t("radiusMd"),
  cursor: "pointer",
  fontSize: 12,
  fontWeight: 600,
  transition: t("transition")
}, O = {
  padding: "9px 12px",
  border: `1px solid ${t("glassBorder")}`,
  borderRadius: t("radiusMd"),
  background: t("bg"),
  color: t("text"),
  fontSize: 13,
  outline: "none"
}, ae = {
  ...O,
  cursor: "pointer"
}, ie = {
  background: t("dangerSubtle"),
  color: t("danger"),
  borderLeft: `3px solid ${t("danger")}`,
  padding: "12px 16px",
  borderRadius: t("radiusMd"),
  marginBottom: 12,
  fontSize: 13
}, L = {
  color: t("textTertiary"),
  padding: "32px 0",
  textAlign: "center",
  fontSize: 13
}, ce = {
  routes: [
    { path: "/admin/health", component: X }
  ]
};
export {
  ce as default
};
