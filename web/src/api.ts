// admin UI 与 core 后端 ext 入口通信的薄封装。
//
// 与 epay 的 api.ts 完全同模式：从 localStorage 取 JWT 作 Bearer 认证，
// 走 /api/v1/ext/airgate-health/* 路径，core 的 extensionProxy 负责转发。
//
// Step 2 变更：
//   - 删除 AccountSummary / AccountHealth / accounts / accountDetail（不再跟踪账号级数据）
//   - GroupHealth 新增 total_probes / success_count / latency_p50/p99 / last_probed_at / last_error
//   - PlatformHealth 的 account_count 改名为 group_count
//   - probeGroup 返回值从"批量聚合"变成"单次探测结果"（分组级探测每次只发一次请求）

const ADMIN_BASE = '/api/v1/ext/airgate-health';

interface CoreApiResp<T> {
  code: number;
  message: string;
  data?: T;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  const token = localStorage.getItem('token');
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const resp = await fetch(ADMIN_BASE + path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  const text = await resp.text();
  let raw: unknown = null;
  try {
    raw = text ? JSON.parse(text) : null;
  } catch {
    /* 非 JSON */
  }

  if (!resp.ok) {
    const wrapper = raw as CoreApiResp<unknown> | null;
    const errMsg =
      wrapper?.message ||
      (raw as { error?: string } | null)?.error ||
      `HTTP ${resp.status}`;
    if (resp.status === 401) {
      localStorage.removeItem('token');
      window.location.href = '/login';
    }
    throw new Error(errMsg);
  }

  const wrapper = raw as CoreApiResp<T> | null;
  if (wrapper && typeof wrapper === 'object' && 'code' in wrapper && 'data' in wrapper) {
    if (wrapper.code !== 0) throw new Error(wrapper.message || '请求失败');
    return wrapper.data as T;
  }
  return raw as T;
}

// ============================================================================
// 数据模型（与后端 aggregator.go 保持一致）
// ============================================================================

export interface DailyPoint {
  date: string;
  total: number;
  success: number;
  uptime_pct: number;
  latency_p95: number;
}

export interface PlatformHealth {
  platform: string;
  window: string;
  /** 本 platform 下分组数 */
  group_count: number;
  uptime_pct: number;
  latency_p95: number;
  status_color: 'green' | 'yellow' | 'red' | 'gray';
  daily?: DailyPoint[];
}

export interface GroupHealth {
  group_id: number;
  group_name: string;
  platform: string;
  /** 来自 core groups.note 的运维备注，只读展示 */
  note?: string;
  window: string;
  total_probes: number;
  success_count: number;
  uptime_pct: number;
  latency_p50: number;
  latency_p95: number;
  latency_p99: number;
  last_probed_at?: string;
  last_error?: string;
  status_color: 'green' | 'yellow' | 'red' | 'gray';
  daily?: DailyPoint[];
}

export interface Overview {
  window: string;
  platforms: PlatformHealth[];
  groups: GroupHealth[];
}

/** 单次分组手动探测的结果（对应 prober.ProbeGroup 返回值） */
export interface GroupProbeResult {
  group_id: number;
  success: boolean;
  latency_ms: number;
  account_id?: number;
  model?: string;
  error_kind?: string;
  error_msg?: string;
  duration_ms: number;
}

export const api = {
  overview: (window: string = '7d') =>
    request<Overview>('GET', `/admin/overview?window=${window}`),

  groups: (window: string = '7d') =>
    request<{ window: string; list: GroupHealth[] }>('GET', `/admin/groups?window=${window}`),

  groupDetail: (id: number, window: string = '7d') =>
    request<GroupHealth>('GET', `/admin/groups/${id}?window=${window}`),

  probeGroup: (groupId: number) =>
    request<GroupProbeResult>('POST', `/admin/probe/group/${groupId}`),
};
