package health

import (
	"context"
	"fmt"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

// hostclient.go：Host.Invoke 的包级助手，供 Prober 与 Aggregator 共用。
//
// 边界约定：分组元信息（name/platform/note）与状态页可见性过滤一律经
// Host.Invoke("groups.list") 从 core 获取，插件不直接查 core 的 groups /
// user_allowed_groups 表——可见性属于 core 的授权职责。

// invokeHost 调一次 Host 方法并解包 payload；host 为 nil 时返回 HostNotReadyError。
func invokeHost(ctx context.Context, host sdk.Host, method string, payload map[string]interface{}) (map[string]interface{}, error) {
	if host == nil {
		return nil, &HostNotReadyError{}
	}
	resp, err := host.Invoke(ctx, sdk.HostInvokeRequest{
		Method:  method,
		Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return map[string]interface{}{}, nil
	}
	if resp.Status == "error" {
		if msg, _ := resp.Payload["message"].(string); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("core 方法 %s 返回错误", method)
	}
	return resp.Payload, nil
}

// fetchGroups 经 groups.list 拉取分组元信息。
//
// publicOnly=false：全部分组（探测循环、admin 视图用）。
// publicOnly=true：core 按状态页可见性过滤——status_visible=true 的公开分组，
// userID>0 时追加该用户被授权的专属分组（user_allowed_groups 判断在 core 完成）。
func fetchGroups(ctx context.Context, host sdk.Host, publicOnly bool, userID int64) ([]hostGroup, error) {
	var payload map[string]interface{}
	if publicOnly {
		payload = map[string]interface{}{"public_only": true}
		if userID > 0 {
			payload["user_id"] = userID
		}
	}
	resp, err := invokeHost(ctx, host, hostMethodGroupsList, payload)
	if err != nil {
		return nil, err
	}
	var groups []hostGroup
	if err := decodeHostValue(resp, &groups, "groups", "items", "data"); err != nil {
		return nil, fmt.Errorf("解析 groups.list 响应失败: %w", err)
	}
	return groups, nil
}
