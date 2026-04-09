import { useEffect, useState } from 'react';
import { cssVar } from '@airgate/theme';
import { api, type Overview, type AccountSummary } from '../api';

// notify 走 core 暴露的统一 toast；core 用 PluginAPIBridge 在 ToastProvider 内
// 把 toast 函数挂到 window.airgate 上。挂之前（极端情况：插件比 bridge 更早渲染、
// 或者插件被独立运行没有 core 宿主）退回到原生 alert，避免静默失败。
type NotifyKind = 'success' | 'error' | 'warning' | 'info';
function notify(kind: NotifyKind, message: string, title?: string) {
  const w = window as unknown as {
    airgate?: { toast?: (kind: NotifyKind, message: string, title?: string) => void };
  };
  if (w.airgate?.toast) {
    w.airgate.toast(kind, message, title);
    return;
  }
  // 兜底：bridge 还没装好就用原生（不应该在生产环境出现）
  // eslint-disable-next-line no-alert
  alert(title ? `${title}\n${message}` : message);
}

/**
 * HealthDashboard 管理员级健康监控面板。
 *
 * 三块内容：
 *   1. 平台维度卡片：每个 platform 一张，展示 uptime % + p95 latency + 状态色
 *   2. 分组聚合表：按 group 维度汇总，附带 core 中维护的 note 备注列
 *   3. 账号明细表：可按 platform 过滤，列出每个账号的 uptime + p95 + 最近探测时间
 *      点击账号名可触发一次手动探测（POST /admin/probe/:id）
 *
 * window 切换器支持 7d / 15d / 30d。
 *
 * 主题：完全使用 @airgate/theme 的 cssVar 引用 core 的主题 token，
 * 所以 dark/light 切换由 core 接管，本插件不写任何固定颜色（除了状态绿/黄/红，
 * 它们语义上与主题无关，使用 success/warning/danger token）。
 */
export default function HealthDashboard() {
  const [windowSel, setWindowSel] = useState<'7d' | '15d' | '30d'>('7d');
  const [overview, setOverview] = useState<Overview | null>(null);
  const [accounts, setAccounts] = useState<AccountSummary[]>([]);
  const [filter, setFilter] = useState('');
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [probingGroup, setProbingGroup] = useState<number | null>(null);

  const reload = async () => {
    setLoading(true);
    setErr(null);
    try {
      const [ov, ac] = await Promise.all([
        api.overview(windowSel),
        api.accounts(windowSel, filter),
      ]);
      // 后端在没有任何账号/探测数据时可能返回 null（Go nil slice），统一兜底为空数组，
      // 避免下方 .map / .length 直接抛 TypeError 把整个面板炸掉。
      setOverview({
        ...ov,
        platforms: ov.platforms ?? [],
        groups: ov.groups ?? [],
      });
      setAccounts(ac.list || []);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [windowSel, filter]);

  const handleProbeGroup = async (groupId: number, groupName: string) => {
    setProbingGroup(groupId);
    try {
      const res = await api.probeGroup(groupId);
      if (res.total === 0) {
        notify('warning', `分组「${groupName}」下没有可探测的账号`);
      } else if (res.failed === 0) {
        notify(
          'success',
          `分组「${groupName}」探测完成：${res.success}/${res.total} 成功，耗时 ${res.duration_ms}ms`,
        );
      } else {
        notify(
          'warning',
          `分组「${groupName}」探测完成：${res.success}/${res.total} 成功，${res.failed} 失败`,
        );
      }
      await reload();
    } catch (e) {
      notify('error', '探测失败: ' + (e instanceof Error ? e.message : String(e)));
    } finally {
      setProbingGroup(null);
    }
  };

  return (
    <div style={containerStyle}>
      <header style={{ marginBottom: 24 }}>
        <h1 style={titleStyle}>健康监控</h1>
        <div style={subtitleStyle}>主动探测各账号连通性，聚合可用率与延迟</div>
      </header>

      <Toolbar
        windowSel={windowSel}
        setWindowSel={setWindowSel}
        filter={filter}
        setFilter={setFilter}
        onReload={reload}
      />

      {err && <div style={errStyle}>错误: {err}</div>}
      {loading && !overview && <div style={emptyStyle}>加载中…</div>}

      {overview && (
        <>
          <h2 style={sectionTitle}>平台总览</h2>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))', gap: 12 }}>
            {overview.platforms.map((p) => (
              <div key={p.platform} style={cardStyle}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 10 }}>
                  <strong style={{ color: cssVar('text') }}>{p.platform}</strong>
                  <Dot color={p.status_color} />
                </div>
                <div style={metricBig}>
                  {p.uptime_pct < 0 ? '—' : p.uptime_pct.toFixed(2) + '%'}
                </div>
                <div style={metricSub}>
                  p95 {p.latency_p95}ms · {p.account_count} 账号
                </div>
              </div>
            ))}
          </div>

          {overview.groups.length > 0 && (
            <>
              <h2 style={sectionTitle}>分组聚合</h2>
              <div style={tableWrapStyle}>
                <table style={tableStyle}>
                  <thead>
                    <tr>
                      <th style={thStyle}>分组</th>
                      <th style={thStyle}>平台</th>
                      <th style={thStyle}>账号数</th>
                      <th style={thStyle}>可用率</th>
                      <th style={thStyle}>p95 延迟</th>
                      <th style={thStyle}>备注</th>
                      <th style={thStyle}>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {overview.groups.map((g) => (
                      <tr key={g.group_id}>
                        <td style={tdStyle}>{g.group_name}</td>
                        <td style={tdStyle}>{g.platform}</td>
                        <td style={tdStyle}>{g.account_count}</td>
                        <td style={tdStyle}>
                          <Dot color={g.status_color} />
                          <span style={{ marginLeft: 8 }}>
                            {g.uptime_pct < 0 ? '—' : g.uptime_pct.toFixed(2) + '%'}
                          </span>
                        </td>
                        <td style={tdStyle}>{g.latency_p95}ms</td>
                        <td style={{ ...tdStyle, color: g.note ? cssVar('text') : cssVar('textTertiary'), maxWidth: 320, whiteSpace: 'pre-wrap' }}>
                          {g.note || '—'}
                        </td>
                        <td style={tdStyle}>
                          <button
                            onClick={() => handleProbeGroup(g.group_id, g.group_name)}
                            disabled={probingGroup === g.group_id}
                            style={primaryBtnStyle}
                          >
                            {probingGroup === g.group_id ? '探测中…' : '立即探测'}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}

          <h2 style={sectionTitle}>账号明细</h2>
          {accounts.length === 0 ? (
            <div style={emptyStyle}>暂无账号数据</div>
          ) : (
            <div style={tableWrapStyle}>
              <table style={tableStyle}>
                <thead>
                  <tr>
                    <th style={thStyle}>名称</th>
                    <th style={thStyle}>平台</th>
                    <th style={thStyle}>状态</th>
                    <th style={thStyle}>可用率</th>
                    <th style={thStyle}>p95 延迟</th>
                    <th style={thStyle}>最近探测</th>
                  </tr>
                </thead>
                <tbody>
                  {accounts.map((a) => (
                    <tr key={a.account_id}>
                      <td style={tdStyle}>{a.account_name}</td>
                      <td style={tdStyle}>{a.platform}</td>
                      <td style={tdStyle}>
                        <StatusBadge status={a.status} />
                      </td>
                      <td style={tdStyle}>{a.uptime_pct < 0 ? '—' : a.uptime_pct.toFixed(2) + '%'}</td>
                      <td style={tdStyle}>{a.latency_p95}ms</td>
                      <td style={tdStyle}>
                        {a.last_probed_at ? new Date(a.last_probed_at).toLocaleString() : '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function Toolbar({
  windowSel,
  setWindowSel,
  filter,
  setFilter,
  onReload,
}: {
  windowSel: '7d' | '15d' | '30d';
  setWindowSel: (w: '7d' | '15d' | '30d') => void;
  filter: string;
  setFilter: (s: string) => void;
  onReload: () => void;
}) {
  return (
    <div style={{ display: 'flex', gap: 12, alignItems: 'center', margin: '0 0 20px' }}>
      <select value={windowSel} onChange={(e) => setWindowSel(e.target.value as '7d' | '15d' | '30d')} style={selectStyle}>
        <option value="7d">最近 7 天</option>
        <option value="15d">最近 15 天</option>
        <option value="30d">最近 30 天</option>
      </select>
      <input
        type="text"
        placeholder="按平台过滤（留空显示全部）"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        style={{ ...inputStyle, flex: 1 }}
      />
      <button onClick={onReload} style={primaryBtnStyle}>
        刷新
      </button>
    </div>
  );
}

function Dot({ color }: { color: 'green' | 'yellow' | 'red' | 'gray' }) {
  // 使用主题语义色：green=success, yellow=warning, red=danger, gray=textTertiary
  const map = {
    green: cssVar('success'),
    yellow: cssVar('warning'),
    red: cssVar('danger'),
    gray: cssVar('textTertiary'),
  };
  return (
    <span
      style={{
        display: 'inline-block',
        width: 10,
        height: 10,
        borderRadius: '50%',
        background: map[color],
        boxShadow: `0 0 0 2px ${cssVar('bgSurface')}`,
        verticalAlign: 'middle',
      }}
    />
  );
}

function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { bg: string; fg: string; label: string }> = {
    active: { bg: cssVar('successSubtle'), fg: cssVar('success'), label: '正常' },
    error: { bg: cssVar('dangerSubtle'), fg: cssVar('danger'), label: '故障' },
    disabled: { bg: cssVar('bgHover'), fg: cssVar('textSecondary'), label: '禁用' },
  };
  const s = map[status] || map.disabled;
  return (
    <span
      style={{
        background: s.bg,
        color: s.fg,
        padding: '2px 10px',
        borderRadius: cssVar('radiusSm'),
        fontSize: 12,
        fontWeight: 500,
      }}
    >
      {s.label}
    </span>
  );
}

// ============================================================================
// Theme-aware styles
// ============================================================================

const containerStyle: React.CSSProperties = {
  maxWidth: 1200,
  margin: '0 auto',
  padding: '24px 24px 48px',
  color: cssVar('text'),
};

const titleStyle: React.CSSProperties = {
  margin: '0 0 6px',
  fontSize: 24,
  fontWeight: 600,
  color: cssVar('text'),
  letterSpacing: '-0.01em',
};

const subtitleStyle: React.CSSProperties = {
  color: cssVar('textSecondary'),
  fontSize: 13,
};

const sectionTitle: React.CSSProperties = {
  margin: '28px 0 12px',
  fontSize: 13,
  fontWeight: 600,
  color: cssVar('textSecondary'),
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
};

const cardStyle: React.CSSProperties = {
  background: cssVar('bgSurface'),
  border: `1px solid ${cssVar('glassBorder')}`,
  borderRadius: cssVar('radiusLg'),
  padding: 16,
};

const metricBig: React.CSSProperties = {
  fontSize: 26,
  fontWeight: 600,
  fontVariantNumeric: 'tabular-nums',
  color: cssVar('text'),
  letterSpacing: '-0.02em',
};

const metricSub: React.CSSProperties = {
  fontSize: 12,
  color: cssVar('textSecondary'),
  marginTop: 6,
};

const tableWrapStyle: React.CSSProperties = {
  background: cssVar('bgSurface'),
  border: `1px solid ${cssVar('glassBorder')}`,
  borderRadius: cssVar('radiusLg'),
  overflow: 'hidden',
};

const tableStyle: React.CSSProperties = {
  width: '100%',
  borderCollapse: 'collapse',
};

const thStyle: React.CSSProperties = {
  textAlign: 'left',
  padding: '12px 16px',
  background: cssVar('bg'),
  fontSize: 11,
  fontWeight: 600,
  color: cssVar('textSecondary'),
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  borderBottom: `1px solid ${cssVar('glassBorder')}`,
};

const tdStyle: React.CSSProperties = {
  padding: '12px 16px',
  borderBottom: `1px solid ${cssVar('borderSubtle')}`,
  fontSize: 13,
  color: cssVar('text'),
};

const primaryBtnStyle: React.CSSProperties = {
  padding: '8px 16px',
  background: cssVar('primary'),
  color: cssVar('textInverse'),
  border: 'none',
  borderRadius: cssVar('radiusMd'),
  cursor: 'pointer',
  fontSize: 12,
  fontWeight: 600,
  transition: cssVar('transition'),
};

const inputStyle: React.CSSProperties = {
  padding: '9px 12px',
  border: `1px solid ${cssVar('glassBorder')}`,
  borderRadius: cssVar('radiusMd'),
  background: cssVar('bg'),
  color: cssVar('text'),
  fontSize: 13,
  outline: 'none',
};

const selectStyle: React.CSSProperties = {
  ...inputStyle,
  cursor: 'pointer',
};

const errStyle: React.CSSProperties = {
  background: cssVar('dangerSubtle'),
  color: cssVar('danger'),
  borderLeft: `3px solid ${cssVar('danger')}`,
  padding: '12px 16px',
  borderRadius: cssVar('radiusMd'),
  marginBottom: 12,
  fontSize: 13,
};

const emptyStyle: React.CSSProperties = {
  color: cssVar('textTertiary'),
  padding: '32px 0',
  textAlign: 'center',
  fontSize: 13,
};
