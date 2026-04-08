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

interface DailyPoint {
  date: string;
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
  daily?: DailyPoint[];
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
        <div style={subtitleStyle}>最近 7 天可用率 · 每分钟自动刷新</div>
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

      {g.daily && g.daily.length > 0 && <DailyGrid daily={g.daily} />}
      <div style={cardMetaStyle}>p95 延迟 {g.latency_p95}ms</div>
    </div>
  );
}

function DailyGrid({ daily }: { daily: DailyPoint[] }) {
  const slots = daily.slice(-90);
  return (
    <div style={{ display: 'flex', gap: 3, height: 26, marginTop: 14 }}>
      {slots.map((d) => {
        const color =
          d.total === 0
            ? cssVar('bgHover')
            : d.uptime_pct >= 99.5
              ? cssVar('success')
              : d.uptime_pct >= 95
                ? cssVar('warning')
                : cssVar('danger');
        return (
          <div
            key={d.date}
            title={`${d.date}: ${
              d.total === 0
                ? '无数据'
                : d.uptime_pct.toFixed(2) + '% (' + d.success + '/' + d.total + ')'
            }`}
            style={{
              flex: 1,
              background: color,
              borderRadius: 2,
              minWidth: 4,
              opacity: d.total === 0 ? 0.4 : 1,
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
