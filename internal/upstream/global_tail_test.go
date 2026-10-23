package upstream

import (
	"encoding/json"
	"testing"
)

// decodeMessages 把出站 body 解回 messages（断言用）。
func decodeMessages(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("out not json: %v", err)
	}
	raw, _ := obj["messages"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, m := range raw {
		mm, _ := m.(map[string]any)
		out = append(out, mm)
	}
	return out
}

func rolesOf(msgs []map[string]any) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m["role"].(string))
	}
	return out
}

// TestEnsureGlobalMessageTailNoChange 已合规序列（tool 后跟 assistant、末尾 user）
// → 原字节切片原样返回（零改动）。
func TestEnsureGlobalMessageTailNoChange(t *testing.T) {
	body := []byte(`{"model":"gemini-3.8-flash","messages":[` +
		`{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"c1","content":"r"},` +
		`{"role":"assistant","content":"done"},` +
		`{"role":"user","content":"next"}]}`)
	out := ensureGlobalMessageTail(body)
	if string(out) != string(body) {
		t.Fatalf("compliant body should be unchanged\n got: %s", out)
	}
}

// TestEnsureGlobalMessageTailTrailingTool 用户实际形态：tool 结果后跟 system、
// 会话以 system 收尾 → 在 tool 后补 assistant 占位、末尾补 user。
func TestEnsureGlobalMessageTailTrailingTool(t *testing.T) {
	body := []byte(`{"messages":[` +
		`{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"c1","content":"r"},` +
		`{"role":"system","content":"ctx"}]}`)
	out := ensureGlobalMessageTail(body)
	got := rolesOf(decodeMessages(t, out))
	want := []string{"user", "assistant", "tool", "assistant", "system", "user"}
	if len(got) != len(want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %v, want %v", got, want)
		}
	}
}

// TestEnsureGlobalMessageTailToolThenUser tool 后直接跟 user（缺模型回合）→
// 在 tool 与 user 之间插入 assistant 占位；末尾已是 user 不追加。
func TestEnsureGlobalMessageTailToolThenUser(t *testing.T) {
	body := []byte(`{"messages":[` +
		`{"role":"tool","tool_call_id":"c1","content":"r"},` +
		`{"role":"user","content":"next"}]}`)
	out := ensureGlobalMessageTail(body)
	got := rolesOf(decodeMessages(t, out))
	want := []string{"tool", "assistant", "user"}
	if len(got) != len(want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %v, want %v", got, want)
		}
	}
}

// TestEnsureGlobalMessageTailConsecutiveTools 并行工具结果（连续 tool）只在
// 最后一条后补一条 assistant，不拆散结果组。
func TestEnsureGlobalMessageTailConsecutiveTools(t *testing.T) {
	body := []byte(`{"messages":[` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}},{"id":"c2","type":"function","function":{"name":"f","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"c1","content":"r1"},` +
		`{"role":"tool","tool_call_id":"c2","content":"r2"}]}`)
	out := ensureGlobalMessageTail(body)
	got := rolesOf(decodeMessages(t, out))
	want := []string{"assistant", "tool", "tool", "assistant", "user"}
	if len(got) != len(want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %v, want %v", got, want)
		}
	}
}

// TestEnsureGlobalMessageTailEndsAssistant 以 assistant 结尾（无工具）→ 只追加 user。
func TestEnsureGlobalMessageTailEndsAssistant(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"}]}`)
	out := ensureGlobalMessageTail(body)
	got := rolesOf(decodeMessages(t, out))
	want := []string{"user", "assistant", "user"}
	if len(got) != len(want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %v, want %v", got, want)
		}
	}
}

// TestEnsureGlobalMessageTailUntouched 非法/非对象 body 原样返回（不二次错误化）。
func TestEnsureGlobalMessageTailUntouched(t *testing.T) {
	for _, body := range [][]byte{
		nil,
		[]byte(`not json`),
		[]byte(`{"messages":[]}`),
		[]byte(`{"messages":"bad"}`),
	} {
		out := ensureGlobalMessageTail(body)
		if string(out) != string(body) {
			t.Fatalf("non-parseable/empty should be untouched: %s -> %s", body, out)
		}
	}
}
