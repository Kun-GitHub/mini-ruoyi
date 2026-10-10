package middleware

import "github.com/gin-gonic/gin"

// cspValue 是给 HTML 页面的 Content-Security-Policy。
//
// 每一条都是在回答「这个头能挡住哪类攻击」，而不是照抄安全清单：
//
//   - script-src 'self'：整份策略里最值钱的一条——挡住注入到页面里的
//     第三方脚本。为此 index.html 里必须永远没有内联 <script>（Vite 构建满足）。
//   - style-src 'self' 'unsafe-inline'：Svelte 组件大量使用 style="..." 属性
//     （menus.svelte、metric-bar.svelte 等），去掉 unsafe-inline 会让页面
//     直接失去样式。样式能造成的破坏远小于脚本，这个妥协是有意的。
//   - img-src 允许 data:：前端目前只用同源图片，留着 data: 是为头像/占位图
//     这类自包含资源，成本可忽略。
//   - object-src 'none' / base-uri 'self' / form-action 'self'：
//     堵住 <object>、<base> 劫持相对路径、表单提交到外部这几条老路。
//   - frame-ancestors 'none'：与 X-Frame-Options: DENY 同义，但覆盖新版浏览器。
//
// 不加 report-uri：报告端点本身需要收集与存储，在 1C1G 上没有收益，
// 浏览器控制台已经足够开发期自查。
const cspValue = "default-src 'self'; base-uri 'self'; object-src 'none'; " +
	"frame-ancestors 'none'; form-action 'self'; " +
	"script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'"

// SecureHeaders 给**每个**响应加安全头，包括错误响应与静态页面。
//
// 必须在最外层注册：放在业务 handler 里就只覆盖到那几个接口，
// 而 XSS、点击劫持、MIME 嗅探这类攻击针对的恰恰是没被想到的那些响应。
//
// HSTS 在纯 HTTP 下会被浏览器忽略（规范要求），所以无条件发出去是安全的；
// 部署在 TLS 终止的反向代理后面时，它反而是唯一能让浏览器记住 HTTPS 的地方。
func SecureHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Content-Security-Policy", cspValue)
		h.Set("X-Content-Type-Options", "nosniff")
		// DENY 而不是 SAMEORIGIN：本服务没有任何需要把自己嵌进 iframe 的场景，
		// DENY 连「同源的恶意页面」一起挡掉。
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// 管理后台用不到摄像头/麦克风/定位，直接关掉，省得某个第三方
		// 资源被攻破后弹出权限请求
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		h.Set("Strict-Transport-Security", "max-age=31536000")
		c.Next()
	}
}
