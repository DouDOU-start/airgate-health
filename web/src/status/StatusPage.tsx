import { useEffect, useState } from 'react';
import { cssVar } from '@airgate/theme';

// 公开状态页（无登录），独立打包，自带 React。
//
// 数据源：/status/api/summary （core 反向代理到本插件）。
// 脱敏原则：只展示 platform 维度的可用率与状态色，不暴露任何 account_id / 错误详情。
//
// 主题：因为这是独立打包的 standalone 页面，没有 core 的 admin shell 注入 CSS vars。
// 但 @airgate/theme 的 cssVar() helper 在 var() 中带有 darkTheme 的 fallback，
// 所以即使 :root 上没有 --ag-* 变量，页面也会以 darkTheme 渲染——这正是我们想要的，
// 公开状态页天然采用品牌深色风格。

interface HourlyPoint {
  hour: string; // ISO8601 e.g. "2026-04-10T08:00:00Z"
  total: number;
  success: number;
  uptime_pct: number;
}

interface GroupHealth {
  group_id: number;
  group_name: string;
  platform: string;
  note?: string;
  uptime_pct: number;
  latency_p95: number;
  status_color: 'green' | 'yellow' | 'red' | 'gray';
  hourly?: HourlyPoint[];
}

interface Summary {
  window: string;
  groups: GroupHealth[];
}

export default function StatusPage() {
  const [data, setData] = useState<Summary | null>(null);
  const [err, setErr] = useState<string | null>(null);

  // 因为 standalone 页面没有 core 的全局 body 样式，我们手动给 document.body
  // 设置背景与字体；只在 mount 时设置一次。
  //
  // 不设 minHeight: 100vh —— 否则配合 pageStyle 的 padding 会让总高度
  // 超过 viewport（content-box 默认下 100vh + padding = viewport + padding），
  // 出现毫无意义的滚动条。让 body 高度由 pageStyle 内的内容自然决定。
  useEffect(() => {
    document.body.style.margin = '0';
    document.body.style.background = cssVar('bgDeep');
    document.body.style.color = cssVar('text');
    document.body.style.fontFamily = cssVar('fontSans');
  }, []);

  const reload = () => {
    fetch('/status/api/summary?window=7d')
      .then((r) => {
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.json();
      })
      .then(setData)
      .catch((e) => setErr(e instanceof Error ? e.message : String(e)));
  };

  useEffect(() => {
    reload();
    const t = setInterval(reload, 60_000);
    return () => clearInterval(t);
  }, []);

  return (
    <div style={pageStyle}>
      <header style={headerStyle}>
        {/* 左：logo + 标题 + 副标题，作为一组品牌区 */}
        <div style={brandColumnStyle}>
          <div style={brandRow}>
            <LogoIcon />
            <h1 style={h1Style}>服务状态</h1>
          </div>
          <div style={subtitleStyle}>
            可用率取最近 7 天 · 趋势图按小时展示最近 168 小时 · 每分钟自动刷新
          </div>
        </div>
        {/* 右：返回控制台。普通 <a> 而非 SPA 路由 —— standalone 页没有 React Router 上下文 */}
        <a href="/" style={backLinkStyle} title="返回控制台">
          ← 返回控制台
        </a>
      </header>

      {err && <div style={errStyle}>加载失败: {err}</div>}

      {!data && !err && <div style={emptyStyle}>加载中…</div>}

      {data && (!data.groups || data.groups.length === 0) && (
        <div style={emptyStyle}>暂无监控数据</div>
      )}

      {data?.groups?.map((g) => <GroupCard key={g.group_id} g={g} />)}

      <footer style={footerStyle}>Powered by airgate-health</footer>
    </div>
  );
}

function LogoIcon() {
  return (
    <svg width="28" height="28" viewBox="0 0 24 24" fill="none" style={{ flexShrink: 0 }}>
      <rect x="1" y="1" width="22" height="22" rx="6" stroke={cssVar('primary')} strokeWidth="1.5" fill="none" />
      <polyline
        points="4,13 8,13 10,8 12,16 14,11 16,13 20,13"
        stroke={cssVar('primary')}
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
    </svg>
  );
}

function GroupCard({ g }: { g: GroupHealth }) {
  const colorMap = {
    green: cssVar('success'),
    yellow: cssVar('warning'),
    red: cssVar('danger'),
    gray: cssVar('textTertiary'),
  };
  const dotColor = colorMap[g.status_color] || colorMap.gray;

  return (
    <div style={cardStyle}>
      <div style={cardHeaderStyle}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, minWidth: 0 }}>
          <span
            style={{
              display: 'inline-block',
              width: 10,
              height: 10,
              borderRadius: '50%',
              background: dotColor,
              boxShadow: `0 0 12px ${dotColor}`,
              flexShrink: 0,
            }}
          />
          <strong style={platformNameStyle}>
            {g.group_name}
            {g.note && (
              <span style={{ marginLeft: 6, fontWeight: 400, color: cssVar('textSecondary') }}>
                ({g.note})
              </span>
            )}
          </strong>
          <span style={{ fontSize: 12, color: cssVar('textTertiary'), flexShrink: 0 }}>· {g.platform}</span>
        </div>
        <div style={uptimeStyle}>
          {g.uptime_pct < 0 ? '—' : g.uptime_pct.toFixed(2) + '%'}
        </div>
      </div>

      <HourlyGrid hourly={g.hourly ?? []} />
      <div style={cardMetaStyle}>p95 延迟 {g.latency_p95}ms</div>
    </div>
  );
}

// HOURLY_BUCKETS 公开状态页固定展示最近 168 小时（7 天）。
// 这个数字必须与后端 handlePublicSummary 传给 GroupHealthList 的 hourlyHours 一致，
// 否则前端时间轴会比后端数据多/少几个柱子，对不齐。
const HOURLY_BUCKETS = 168;

// HourlyGrid 渲染最近 168 小时的可用率方格。
//
// 关键设计：**前端生成完整的时间轴**，再从后端返回的稀疏 hourly 数据里查找对应桶。
// 后端只返回有探测数据的小时（COUNT > 0），完全没探测的小时不在结果里——前端
// 必须把这些"空洞"也渲染成灰色柱子，否则可用率数据少几小时会让格子数量
// 在不同 group 之间不一致，看起来很乱。
//
// 时间轴起点：当前时刻向前推 168 小时，按小时对齐。
function HourlyGrid({ hourly }: { hourly: HourlyPoint[] }) {
  // 1. 把后端结果按 hour 字符串建索引
  const byHour = new Map<string, HourlyPoint>();
  for (const h of hourly) {
    byHour.set(h.hour, h);
  }

  // 2. 生成完整的 168 个小时刻度（向前对齐到整点 UTC）
  const slots: Array<{ key: string; t: Date; hp: HourlyPoint | undefined }> = [];
  const now = new Date();
  const alignedNowUTC = new Date(Date.UTC(
    now.getUTCFullYear(),
    now.getUTCMonth(),
    now.getUTCDate(),
    now.getUTCHours(),
  ));
  for (let i = HOURLY_BUCKETS - 1; i >= 0; i--) {
    const t = new Date(alignedNowUTC.getTime() - i * 3600 * 1000);
    const key = t.toISOString().replace(/\.\d{3}Z$/, 'Z');
    slots.push({ key, t, hp: byHour.get(key) });
  }

  // 3. 按本地日期分组（每天一大格，内部按小时分小格）
  const days: Array<{ label: string; slots: typeof slots }> = [];
  let currentDay = '';
  for (const slot of slots) {
    const localDate = slot.t.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' });
    if (localDate !== currentDay) {
      currentDay = localDate;
      days.push({ label: localDate, slots: [] });
    }
    days[days.length - 1].slots.push(slot);
  }

  return (
    <div style={{ marginTop: 14 }}>
      {/* 按天分格，天之间用细竖线分隔 */}
      <div style={{ display: 'flex', gap: 1, height: 40 }}>
        {days.map((day, di) => (
          <div
            key={day.label}
            style={{
              display: 'flex',
              gap: 1,
              flex: day.slots.length,
              borderRight: di < days.length - 1 ? '1px solid rgba(255,255,255,0.15)' : undefined,
              paddingRight: di < days.length - 1 ? 2 : undefined,
            }}
          >
            {day.slots.map(({ key, hp }) => {
              const total = hp?.total ?? 0;
              const uptime = hp?.uptime_pct ?? -1;
              const barColor =
                total === 0
                  ? '#ffffff'
                  : uptime >= 99.5
                    ? '#22c55e'
                    : uptime >= 95
                      ? '#eab308'
                      : '#ef4444';
              const localLabel = new Date(key).toLocaleString();
              const tooltip =
                total === 0
                  ? `${localLabel}: 无数据`
                  : `${localLabel}: ${uptime.toFixed(2)}% (${hp!.success}/${hp!.total})`;
              return (
                <div
                  key={key}
                  title={tooltip}
                  style={{
                    flex: 1,
                    background: barColor,
                    borderRadius: 2,
                    opacity: total === 0 ? 0.12 : 1,
                  }}
                />
              );
            })}
          </div>
        ))}
      </div>
      {/* 日期轴标签 */}
      <div style={hourlyAxisStyle}>
        {days.map((day, i) => (
          <span key={day.label} style={{ flex: day.slots.length, textAlign: 'center', fontSize: 10 }}>
            {i === days.length - 1 ? '今天' : day.label}
          </span>
        ))}
      </div>
    </div>
  );
}

// ============================================================================
// Theme-aware styles
// ============================================================================

const pageStyle: React.CSSProperties = {
  fontFamily: cssVar('fontSans'),
  maxWidth: 960,
  margin: '0 auto',
  padding: '48px 24px 48px',
  color: cssVar('text'),
  // 故意不设 minHeight: 100vh —— 见 StatusPage 顶部的 useEffect 注释
  // 整页高度由内容自然撑开，避免不必要的滚动条
};

const headerStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'flex-start',
  justifyContent: 'space-between',
  gap: 16,
  marginBottom: 28,
  paddingBottom: 20,
  borderBottom: `1px solid ${cssVar('glassBorder')}`,
};

const brandColumnStyle: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 10,
  minWidth: 0, // 允许内部 truncate 时不撑破 flex container
};

const brandRow: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 14,
};

const h1Style: React.CSSProperties = {
  fontSize: 26,
  fontWeight: 600,
  margin: 0,
  color: cssVar('text'),
  letterSpacing: '-0.02em',
  lineHeight: 1.2,
};

const subtitleStyle: React.CSSProperties = {
  color: cssVar('textSecondary'),
  fontSize: 12,
  lineHeight: 1.5,
};

const backLinkStyle: React.CSSProperties = {
  // 不再用 marginLeft:auto，header 已经是 space-between
  flexShrink: 0,
  fontSize: 12,
  color: cssVar('textSecondary'),
  textDecoration: 'none',
  padding: '8px 14px',
  borderRadius: 8,
  border: `1px solid ${cssVar('glassBorder')}`,
  background: cssVar('bgSurface'),
  transition: 'all 0.15s',
  display: 'inline-flex',
  alignItems: 'center',
  gap: 4,
  lineHeight: 1,
};

const hourlyAxisStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  marginTop: 8,
  fontSize: 11,
  color: cssVar('textTertiary'),
  fontVariantNumeric: 'tabular-nums',
};

const hourlyAxisDividerStyle: React.CSSProperties = {
  flex: 1,
  height: 1,
  background: cssVar('glassBorder'),
  margin: '0 12px',
};

const cardStyle: React.CSSProperties = {
  background: cssVar('bgSurface'),
  border: `1px solid ${cssVar('glassBorder')}`,
  borderRadius: cssVar('radiusLg'),
  padding: 20,
  marginBottom: 12,
  boxShadow: cssVar('shadowSm'),
};

const cardHeaderStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
};

const platformNameStyle: React.CSSProperties = {
  fontSize: 16,
  fontWeight: 600,
  color: cssVar('text'),
};

const uptimeStyle: React.CSSProperties = {
  fontSize: 22,
  fontWeight: 600,
  fontVariantNumeric: 'tabular-nums',
  color: cssVar('text'),
  letterSpacing: '-0.02em',
};

const cardMetaStyle: React.CSSProperties = {
  fontSize: 12,
  color: cssVar('textSecondary'),
  marginTop: 12,
};

const noteStyle: React.CSSProperties = {
  marginTop: 6,
  marginLeft: 22, // 与状态点对齐
  fontSize: 12,
  color: cssVar('textSecondary'),
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-word',
};

const errStyle: React.CSSProperties = {
  background: cssVar('dangerSubtle'),
  color: cssVar('danger'),
  borderLeft: `3px solid ${cssVar('danger')}`,
  padding: '14px 18px',
  borderRadius: cssVar('radiusMd'),
  marginBottom: 16,
  fontSize: 14,
};

const emptyStyle: React.CSSProperties = {
  color: cssVar('textTertiary'),
  padding: '40px 0',
  textAlign: 'center',
  fontSize: 14,
};

const footerStyle: React.CSSProperties = {
  marginTop: 40,
  paddingTop: 24,
  borderTop: `1px solid ${cssVar('borderSubtle')}`,
  color: cssVar('textTertiary'),
  fontSize: 12,
  textAlign: 'center',
};
