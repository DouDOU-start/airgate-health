package health

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CoreClient 封装对 core admin API 的回调（仅用 admin API key 鉴权）。
//
// 为什么不直接调 gateway 插件：插件之间不能 gRPC 互调。复用 core 的 TestAccount
// 既保留了"如何探测 provider"的知识在各 gateway 插件里（零重复），又不需要新增
// SDK 接口或 RPC。
type CoreClient struct {
	baseURL     string        // 如 http://127.0.0.1:8080
	adminAPIKey string        // admin-xxx
	httpClient  *http.Client  // 复用连接池
	timeout     time.Duration // 单次探测超时
}

// NewCoreClient 构造 client；timeout 为单次 TestAccount 调用的总耗时上限。
func NewCoreClient(baseURL, adminAPIKey string, timeout time.Duration) *CoreClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &CoreClient{
		baseURL:     strings.TrimRight(baseURL, "/"),
		adminAPIKey: adminAPIKey,
		timeout:     timeout,
		httpClient: &http.Client{
			Timeout: timeout + 2*time.Second, // 比 ctx 超时稍大，让 server 端的 ctx cancel 先生效
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// ProbeResult 一次 TestAccount 的结果，对应 health_probes 表的一行。
type ProbeResult struct {
	Success    bool
	LatencyMS  int
	StatusCode int    // 上游 HTTP 状态码（来自 SSE 中的 test_complete 或 HTTP response）
	ErrorKind  string // timeout / 4xx / 5xx / network / auth / unknown
	ErrorMsg   string // 截断到 512 字节
}

// TestAccount 调用 core 的 POST /api/v1/admin/accounts/:id/test，
// 解析 SSE 流，定位 test_complete 事件并据此构造 ProbeResult。
//
// 实现要点：
//  1. 用一个独立的 ctx with timeout，避免 prober 全局 ctx 被单个慢探测拖累。
//  2. 总耗时（HTTP round-trip）作为 latency_ms；test_complete 中的 success 决定 success 字段。
//  3. SSE 中可能包含上游错误信息，简单地把最后一个 error 字段截断作为 ErrorMsg。
func (c *CoreClient) TestAccount(ctx context.Context, accountID int64) ProbeResult {
	start := time.Now()

	probeCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	url := fmt.Sprintf("%s/api/v1/admin/accounts/%d/test", c.baseURL, accountID)
	body := bytes.NewBufferString(`{}`) // 不指定 model_id，由 core 选默认模型

	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, url, body)
	if err != nil {
		return errResult("network", err, time.Since(start))
	}
	req.Header.Set("Authorization", "Bearer "+c.adminAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// context.DeadlineExceeded → timeout
		if probeCtx.Err() == context.DeadlineExceeded {
			return ProbeResult{
				LatencyMS: int(time.Since(start).Milliseconds()),
				ErrorKind: "timeout",
				ErrorMsg:  truncateErr("探测超时"),
			}
		}
		return errResult("network", err, time.Since(start))
	}
	defer resp.Body.Close()

	// 401/403：admin API key 失效
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ProbeResult{
			LatencyMS:  int(time.Since(start).Milliseconds()),
			StatusCode: resp.StatusCode,
			ErrorKind:  "auth",
			ErrorMsg:   truncateErr("回调 core 鉴权失败：admin_api_key 失效或权限不足"),
		}
	}
	// 非 SSE 返回（4xx/5xx 通常是 JSON 错误）
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		kind := categorize(resp.StatusCode)
		return ProbeResult{
			LatencyMS:  int(time.Since(start).Milliseconds()),
			StatusCode: resp.StatusCode,
			ErrorKind:  kind,
			ErrorMsg:   truncateErr(fmt.Sprintf("core 返回 %d: %s", resp.StatusCode, string(raw))),
		}
	}

	// 解析 SSE
	success, errMsg := parseSSEForCompletion(resp.Body)
	latency := int(time.Since(start).Milliseconds())

	if success {
		return ProbeResult{
			Success:    true,
			LatencyMS:  latency,
			StatusCode: 200,
		}
	}

	kind := "unknown"
	lower := strings.ToLower(errMsg)
	switch {
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		kind = "timeout"
	case strings.Contains(lower, "401") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "auth"):
		kind = "auth"
	case strings.Contains(lower, "429") || strings.Contains(lower, "rate") || strings.Contains(lower, "quota"):
		kind = "quota"
	case strings.Contains(lower, "4"):
		kind = "4xx"
	case strings.Contains(lower, "5"):
		kind = "5xx"
	case strings.Contains(lower, "connection") || strings.Contains(lower, "network"):
		kind = "network"
	}
	return ProbeResult{
		Success:    false,
		LatencyMS:  latency,
		StatusCode: 200, // SSE 是 200，但内容里告知失败
		ErrorKind:  kind,
		ErrorMsg:   truncateErr(errMsg),
	}
}

// parseSSEForCompletion 流式解析 SSE，定位 test_complete 事件。
//
// SSE 协议：每个事件由若干 "field: value\n" 行组成，事件之间空行分隔。
// core 的 sendSSEEvent 只发 "data: {...}\n\n"，所以我们只关心 data: 行。
//
// 返回 (success, errorMessage)；若没遇到 test_complete 而流先结束，视为失败。
func parseSSEForCompletion(body io.Reader) (bool, string) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20) // 最长 1MB 的单事件

	var lastErr string
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		if ev.Error != "" {
			lastErr = ev.Error
		}
		if ev.Type == "test_complete" {
			if ev.Success {
				return true, ""
			}
			if ev.Error != "" {
				return false, ev.Error
			}
			if lastErr != "" {
				return false, lastErr
			}
			return false, "探测失败（无详细错误）"
		}
	}
	if err := scanner.Err(); err != nil {
		return false, "读取 SSE 流失败: " + err.Error()
	}
	if lastErr != "" {
		return false, lastErr
	}
	return false, "SSE 流结束但未收到 test_complete 事件"
}

func errResult(kind string, err error, dur time.Duration) ProbeResult {
	return ProbeResult{
		LatencyMS: int(dur.Milliseconds()),
		ErrorKind: kind,
		ErrorMsg:  truncateErr(err.Error()),
	}
}

func categorize(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code == 429:
		return "quota"
	case code >= 400:
		return "4xx"
	default:
		return "unknown"
	}
}

func truncateErr(s string) string {
	const max = 512
	if len(s) <= max {
		return s
	}
	return s[:max]
}
