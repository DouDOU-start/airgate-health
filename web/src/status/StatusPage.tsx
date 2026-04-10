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
  useEffect(() => {
    document.body.style.margin = '0';
    document.body.style.background = cssVar('bgDeep');
    document.body.style.color = cssVar('text');
    document.body.style.fontFamily = cssVar('fontSans');
    document.body.style.minHeight = '100vh';
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
        <div style={brandRow}>
          <div style={logoMark} />
          <h1 style={h1Style}>服务状态</h1>
        </div>
        <div style={subtitleStyle}>可用率取最近 7 天 · 趋势图按小时展示最近 168 小时 · 每分钟自动刷新</div>
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
  const slots: Array<{ key: string; hp: HourlyPoint | undefined }> = [];
  const now = new Date();
  // 对齐到当前小时的整点（UTC）
  const alignedNowUTC = new Date(Date.UTC(
    now.getUTCFullYear(),
    now.getUTCMonth(),
    now.getUTCDate(),
    now.getUTCHours(),
  ));
  for (let i = HOURLY_BUCKETS - 1; i >= 0; i--) {
    const t = new Date(alignedNowUTC.getTime() - i * 3600 * 1000);
    // 与后端 to_char 输出对齐：YYYY-MM-DDTHH:00:00Z
    const key = t.toISOString().replace(/:\d{2}\.\d{3}Z$/, ':00:00Z');
    slots.push({ key, hp: byHour.get(key) });
  }

  return (
    <div style={{ display: 'flex', gap: 1, height: 26, marginTop: 14 }}>
      {slots.map(({ key, hp }) => {
        const total = hp?.total ?? 0;
        const uptime = hp?.uptime_pct ?? -1;
        const color =
          total === 0
            ? cssVar('bgHover')
            : uptime >= 99.5
              ? cssVar('success')
              : uptime >= 95
                ? cssVar('warning')
                : cssVar('danger');
        // hover tooltip 转换 UTC 时间到本地，便于运维直接读
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
              background: color,
              borderRadius: 1,
              minWidth: 2,
              opacity: total === 0 ? 0.4 : 1,
            }}
          />
        );
      })}
    </div>
  );
}

// ============================================================================
// Theme-aware styles
// ============================================================================

const pageStyle: React.CSSProperties = {
  fontFamily: cssVar('fontSans'),
  maxWidth: 800,
  margin: '0 auto',
  padding: '48px 24px 64px',
  color: cssVar('text'),
  minHeight: '100vh',
};

const headerStyle: React.CSSProperties = {
  marginBottom: 32,
};

const brandRow: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 14,
  marginBottom: 8,
};

const logoMark: React.CSSProperties = {
  width: 28,
  height: 28,
  borderRadius: 8,
  background: `linear-gradient(135deg, ${cssVar('primary')}, ${cssVar('primaryHover')})`,
  boxShadow: cssVar('shadowGlow'),
};

const h1Style: React.CSSProperties = {
  fontSize: 28,
  fontWeight: 600,
  margin: 0,
  color: cssVar('text'),
  letterSpacing: '-0.02em',
};

const subtitleStyle: React.CSSProperties = {
  color: cssVar('textSecondary'),
  fontSize: 13,
  marginLeft: 42,
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
