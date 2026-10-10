package reqid

import (
	"context"
	"testing"
)

func TestNewIs16HexChars(t *testing.T) {
	id := New()
	if !Valid(id) {
		t.Fatalf("New() = %q，不在允许格式内", id)
	}
	if len(id) != 16 {
		t.Errorf("长度 = %d，期望 16", len(id))
	}

	// 连续取几个，不该撞车
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		seen[New()] = true
	}
	if len(seen) != 100 {
		t.Errorf("100 次生成只得到 %d 个不同的 ID", len(seen))
	}
}

// TestValidRejectsLogInjection 是日志注入的防线。
//
// ID 会被写进日志行，放行换行符与引号就等于允许上游往日志里插入任意记录——
// 排查问题时看到的「错误」可能根本不存在。
func TestValidRejectsLogInjection(t *testing.T) {
	long := "01234567890123456789012345678901234567890123456789012345678901234"
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"abc123", true},
		{"a.b_c-d", true},
		{"0123456789abcdef", true},
		{"", false},              // 空串交给 New()
		{"with space", false},    // 空格是日志的分隔符
		{"line\nbreak", false},   // 换行 = 插入一条新记录
		{"quote\"fake=1", false}, // 引号能伪造键值对
		{"tab\tsep", false},      // 控制字符
		{long, false},            // 超过 64 位
	} {
		if got := Valid(tc.id); got != tc.want {
			t.Errorf("Valid(%q) = %v，期望 %v", tc.id, got, tc.want)
		}
	}
}

func TestFromContext(t *testing.T) {
	if got := From(context.Background()); got != "" {
		t.Errorf("空上下文返回 %q，期望空串", got)
	}

	ctx := WithContext(context.Background(), "abc123")
	if got := From(ctx); got != "abc123" {
		t.Errorf("From = %q，期望 abc123", got)
	}
}
