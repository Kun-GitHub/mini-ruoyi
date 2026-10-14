// 有问题欢迎联系 hot_kun@hotmail.com
// Package reqid 生成并透传请求 ID（correlation id）。
//
// 独立成包而不是住在 middleware 里：写响应的 httpx 也要能拿到这个 ID，
// 而 middleware 已经 import 了 httpx——httpx 再反过来 import middleware 就成了循环。
// 一个只依赖标准库的小包放在中间，两边都能 import。
package reqid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

// Header 是请求 ID 的传输与响应头名字。
//
// 用 `X-Request-Id` 而不是自造名字：nginx、API 网关、OpenTelemetry 探针
// 用的都是它，透传同名头就能把服务端日志和上游访问日志串起来。
const Header = "X-Request-Id"

// ctxKey 是 id 在 context 里的键。结构体类型作键，避免与别的包撞字符串。
type ctxKey struct{}

// New 返回一个 16 位十六进制随机 ID。
//
// 8 字节 = 4.6e9 亿次生成后碰撞概率才到 1%，而这里只需要在**单机日志里**
// 唯一区分并发请求，绰绰有余；再长只是让日志更难读。
func New() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 读不出随机数只可能是系统级故障；退化成全零也比 panic 好——
		// 这个 ID 只用于日志关联，不该让请求因此失败。
		return "0000000000000000"
	}
	return hex.EncodeToString(b[:])
}

// valid 是允许透传的 ID 格式。
//
// **必须校验再透传**：这个值会被写进日志行，而日志会被 logfmt 解析。
// 放行任意字符意味着上游一行 `x\nlevel=ERROR msg="..."` 就能往日志里
// 插入假记录（日志注入）。限制成常见字符集后，注入在格式层面就不可能。
var valid = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Valid 判断上游传来的 ID 能否直接沿用；不合格的应当用 New() 重新生成。
func Valid(id string) bool { return valid.MatchString(id) }

// WithContext 把 ID 放进请求上下文。
func WithContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From 取出上下文里的 ID；没有则返回空串。
//
// 返回空串而不是随机生成一个：调用方（日志）拿到的应当是**这条请求**的 ID，
// 在没有的地方现造一个只会让日志里出现两个互不相干的 ID。
func From(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}
