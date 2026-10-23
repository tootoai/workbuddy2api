// global_tail.go 把 global（workbuddy.ai / gemini）出站消息序列的尾巴补成上游
// 接受的形状。
//
// 背景（2026-10-04 实测，真实 Codex 会话复现）：
//
// global 后端对 OpenAI wire 的消息序列有两条硬校验，CN（deepseek）完全不校验：
//
//  1. 每条 role:tool 的结果消息之后必须紧跟一条 assistant 消息（模型对结果的回合）；
//     tool 后直接跟 user / system 或位于末尾 → 400 code=11133 model_param_invalid
//     （"the request parameters were rejected by the model provider"）。
//  2. 最后一条非 system 消息必须是 user；以 assistant / tool 结尾同样 400。
//
// 症状：Codex 这类客户端每轮工具调用的请求都以 function_call_output 结尾（Responses
// API 的正常形态），经 /v1/responses→chat 转换后消息以 tool 收尾 → 上游对**每一条**
// 请求都回 400 → 号池连续失败进冷却、清空 → 客户端只看到 503。CN 不受影响，所以
// 表现出来就是「国内版正常、国际版全挂」。
//
// 处理：只在**缺失处插入占位**，不删除、不重排任何原消息——
//   · role:tool 且下一条不是 tool/assistant 时（含位于末尾），在其后插一条
//     assistant（content 单空格；上游校验 len>0 且不 trim，空串过不了）；
//   · 末尾最后一条非 system 消息不是 user 时（assistant / tool），追加一条
//     user（content "continue"），让模型对工具结果/上下文给出回复。
//
// 正常以 user 结尾、工具结果已按 OpenAI 协议配对（tool 后跟 assistant）的请求
// 零改动（无插入时返回原字节切片）。
package upstream

import "encoding/json"

// ensureGlobalMessageTail 仅对 global realm 调用（CN 零改动；调用点在 client.prepareBody）。
func ensureGlobalMessageTail(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body
	}
	msgs, ok := obj["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return body
	}
	out := make([]any, 0, len(msgs)+2)
	changed := false
	for i, raw := range msgs {
		out = append(out, raw)
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role != "tool" {
			continue
		}
		// 下一跳的角色：同一批并行 tool 结果（连续 role:tool）只在最后一条后补，
		// 已正确跟随 assistant 的不补。
		next := ""
		if i+1 < len(msgs) {
			if n, ok := msgs[i+1].(map[string]any); ok {
				next, _ = n["role"].(string)
			}
		}
		if next == "tool" || next == "assistant" {
			continue
		}
		out = append(out, map[string]any{"role": "assistant", "content": " "})
		changed = true
	}
	if lastNonSystemRole(out) != "user" {
		out = append(out, map[string]any{"role": "user", "content": "continue"})
		changed = true
	}
	if !changed {
		return body
	}
	obj["messages"] = out
	rewritten, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return rewritten
}

// lastNonSystemRole 返回最后一条非 system 消息的角色；没有这样的消息时返回空串
// （全 system 序列也按需要补一条 user，让模型有可回应的回合）。
func lastNonSystemRole(msgs []any) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if m, ok := msgs[i].(map[string]any); ok {
			if role, _ := m["role"].(string); role != "system" {
				return role
			}
		}
	}
	return ""
}
