// Package httpx 统一 HTTP 响应信封。
//
// 独立成包而非放在 handler 里：middleware 和 httpserver 同样需要写响应
// （401/403/404/429/503），若信封住在 handler 中，这些包就得反向依赖 handler。
//
// 语言策略：后端只产出 i18n 键，不产出面向用户的文案。文案由前端按
// zh-CN / en-US 字典翻译，后端因此保持语言中立。
package httpx

import (
	"errors"
	"log"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"mini-ruoyi/internal/domain"
)

// 错误键。新增或改名时必须同步前端字典 web/src/lib/i18n/zh-CN.ts。
const (
	KeyNotFound           = "error.notFound"
	KeyValidationFailed   = "error.validationFailed"
	KeyBodyTooLarge       = "error.bodyTooLarge"
	KeyMalformedBody      = "error.malformedBody"
	KeyInternal           = "error.internal"
	KeyTooManyRequests    = "error.tooManyRequests"
	KeyServiceUnavailable = "error.serviceUnavailable"
	KeyInvalidID          = "error.invalidId"
	// KeyHasDependents 用于删除有子数据的资源时返回 409，响应体带影响面，
	// 前端据此弹确认框而不是报错。详见 docs/architecture.md §3.4。
	KeyHasDependents    = "error.hasDependents"
	KeyProtected        = "error.protected"
	KeyCannotDeleteSelf = "error.cannotDeleteSelf"
	KeyLastAdmin        = "error.lastAdmin"
	KeyInvalidParent    = "error.invalidParent"
	KeyBadCredentials   = "error.badCredentials"
	KeyAccountDisabled  = "error.accountDisabled"
	KeyUnauthorized     = "error.unauthorized"
	KeyForbidden        = "error.forbidden"
	KeyCSRFInvalid      = "error.csrfInvalid"
	// KeyBackendUnreachable 表示「响应不是本服务的信封格式」——请求被代理/网关拦下，
	// 或者后端进程根本没起来。
	//
	// 这个键由**前端**产生（web/src/lib/api/client.ts），后端从不返回它。
	// 它定义在这里，是为了让 TestFrontendDictCoversErrorKeys 能覆盖到：
	// 那个用例用正则扫本文件的全部 error.* 键，再断言前端字典里有对应文案。
	// 前端字典里若少了它，界面会直接把 "error.backendUnreachable" 显示给用户。
	KeyBackendUnreachable = "error.backendUnreachable"
	// KeyFrontendDisabled 表示后端以纯 API 模式运行、前端未部署。
	// 常见于用 IDE 或 go run 启动、且没有 bin/web 的开发场景。
	KeyFrontendDisabled = "error.frontendDisabled"
	// KeyCannotKickSelf 不允许踢掉自己当前这条会话。
	KeyCannotKickSelf = "error.cannotKickSelf"
	// KeyInvalidJobCron cron 表达式无法解析。
	KeyInvalidJobCron = "error.invalidJobCron"
	// KeyWrongOldPassword 修改自己密码时旧密码不对。
	KeyWrongOldPassword = "error.wrongOldPassword"
	// 上传相关的容量限制。
	KeyFileTooLarge    = "error.fileTooLarge"
	KeyQuotaExceeded   = "error.quotaExceeded"
	KeyInvalidFile     = "error.invalidFile"
	KeyDuplicate       = "error.duplicate"
	KeyInvalidPermCode = "error.invalidPermCode"
)

// SuccessMsg 是成功响应的 msg 值。它不是 i18n 键，前端不需要翻译。
const SuccessMsg = "ok"

// Response 是所有接口的统一响应体。
//
// 语义由 HTTP 状态码承载，`Code` 恒等于同一个状态码。
//
// 为什么是状态码的副本而不是 0/1：Go 的 int 零值是 0，而本字段没有 omitempty，
// 所以用 0 表示成功时，任何「忘了给 Code 赋值」的新代码路径都会**静默返回成功**：
//
//	json.Marshal(Response{Msg: "ok", Data: x})  // 忘了设 Code
//	→ {"code":0,"msg":"ok","data":...}      // 前端判成成功
//
// 取状态码做值，零值 0 就不可能是合法值。更重要的是：调用方**不需要也不应该**
// 自己填 Code —— 一律走 write()，它把状态码写进去，所以两者不可能分歧。
type Response struct {
	Code   int          `json:"code"`
	Msg    string       `json:"msg"` // 成功为 "ok"，失败为 i18n 键（如 error.notFound）
	Data   any          `json:"data,omitempty"`
	Errors []FieldError `json:"errors,omitempty"`
}

// FieldError 是一条字段级校验失败。
//
// 后端只给出「哪个字段、违反了哪条规则、规则参数是多少」，
// 文案由前端用 rule + param 拼装，字段称谓也由前端提供。
type FieldError struct {
	Field string `json:"field"`
	Rule  string `json:"rule"`
	Param string `json:"param,omitempty"`
}

// write 是写出信封的唯一出口。
//
// 它把 HTTP 状态码写进 Code，所以两者在结构上不可能分歧——调用方根本碰不到 Code。
// 分开写的话（每处自己填 Code: 200 / Code: 1）迟早会有一处填错，
// 而填错的表现是「HTTP 说 403，body 说成功」。
func write(c *gin.Context, status int, resp Response) {
	resp.Code = status
	c.JSON(status, resp)
}

func Success(c *gin.Context, data any) {
	write(c, http.StatusOK, Response{Msg: SuccessMsg, Data: data})
}

// resultKeyCtx 是失败键在请求上下文里的存放位置。
//
// 放在这里而不是让操作日志中间件去包装 ResponseWriter 抽 msg：
// 写响应的这一刻才知道键，顺手记下来最准，也最省事。
const resultKeyCtx = "httpx.result_key"

// ResultKey 取出本次请求失败时用的 i18n 键；成功请求返回空串。
func ResultKey(c *gin.Context) string {
	if v, ok := c.Get(resultKeyCtx); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Fail 用 i18n 键而非文案写失败响应。
func Fail(c *gin.Context, httpStatus int, msgKey string) {
	c.Set(resultKeyCtx, msgKey)
	write(c, httpStatus, Response{Msg: msgKey})
}

// FailValidation 写参数校验失败响应，msg 固定为 KeyValidationFailed，
// 细节在 errors 数组里。
func FailValidation(c *gin.Context, fields []FieldError) {
	c.Set(resultKeyCtx, KeyValidationFailed)
	write(c, http.StatusBadRequest, Response{
		Msg:    KeyValidationFailed,
		Errors: fields,
	})
}

// errorMapping 是领域错误 → (HTTP 状态码, i18n 键) 的唯一映射表。
//
// 抽成数据而不是写在 switch 里，是为了让「记日志只要键」的调用方（登录日志）
// 也能用它。两处各写一份的话，迟早会有一处漏改，日志里的原因和界面上的提示就对不上了。
var errorMapping = []struct {
	target error
	status int
	key    string
}{
	{domain.ErrNotFound, http.StatusNotFound, KeyNotFound},
	{domain.ErrProtected, http.StatusForbidden, KeyProtected},
	{domain.ErrCannotDeleteSelf, http.StatusForbidden, KeyCannotDeleteSelf},
	{domain.ErrCannotKickSelf, http.StatusForbidden, KeyCannotKickSelf},
	{domain.ErrLastAdmin, http.StatusConflict, KeyLastAdmin},
	{domain.ErrInvalidParent, http.StatusBadRequest, KeyInvalidParent},
	{domain.ErrInvalidPermCode, http.StatusBadRequest, KeyInvalidPermCode},
	{domain.ErrInvalidJobCron, http.StatusBadRequest, KeyInvalidJobCron},
	{domain.ErrWrongOldPassword, http.StatusBadRequest, KeyWrongOldPassword},
	// 上传超限用 413：和「请求体过大」同类，客户端据此提示「文件太大」
	{domain.ErrFileTooLarge, http.StatusRequestEntityTooLarge, KeyFileTooLarge},
	{domain.ErrQuotaExceeded, http.StatusRequestEntityTooLarge, KeyQuotaExceeded},
	{domain.ErrInvalidFile, http.StatusBadRequest, KeyInvalidFile},
	{domain.ErrBadCredentials, http.StatusUnauthorized, KeyBadCredentials},
	{domain.ErrUnauthorized, http.StatusUnauthorized, KeyUnauthorized},
	{domain.ErrAccountDisabled, http.StatusForbidden, KeyAccountDisabled},
	{domain.ErrForbidden, http.StatusForbidden, KeyForbidden},
	{domain.ErrCSRFInvalid, http.StatusForbidden, KeyCSRFInvalid},
}

// ErrorStatusKey 返回错误对应的 HTTP 状态码与 i18n 键，不写响应。
func ErrorStatusKey(err error) (int, string) {
	for _, m := range errorMapping {
		if errors.Is(err, m.target) {
			return m.status, m.key
		}
	}
	return http.StatusInternalServerError, KeyInternal
}

// ErrorKeyOf 只取 i18n 键，供审计日志使用。
func ErrorKeyOf(err error) string {
	_, key := ErrorStatusKey(err)
	return key
}

// FailFromError 把领域错误映射成 HTTP 响应。
//
// 带数据的两种错误（有依赖 / 唯一冲突）在这里单独处理，
// 其余走 errorMapping 那张表。
func FailFromError(c *gin.Context, err error) {
	var (
		depErr *domain.DependentsError
		dupErr *domain.DuplicateError
	)

	switch {
	case errors.As(err, &depErr):
		// 这不是错误提示，而是「请确认」：前端收到它应弹确认框并展示影响面，
		// 用户确认后带 ?cascade=true 重发。详见 docs/architecture.md §3.4。
		c.Set(resultKeyCtx, KeyHasDependents)
		write(c, http.StatusConflict, Response{Msg: KeyHasDependents, Data: depErr.Impact})

	case errors.As(err, &dupErr):
		// 复用字段级错误的形状，前端能把「用户名已存在」高亮到对应输入框
		c.Set(resultKeyCtx, KeyDuplicate)
		write(c, http.StatusConflict, Response{
			Msg:    KeyDuplicate,
			Errors: []FieldError{{Field: dupErr.Field, Rule: "unique"}},
		})

	default:
		status, key := ErrorStatusKey(err)
		// 5xx 必须落日志：客户端只会拿到一个错误键，排查只能靠服务端日志
		if status >= http.StatusInternalServerError {
			log.Printf("%s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		}
		Fail(c, status, key)
	}
}

// FailBindError 处理请求体解析/校验失败。
//
// 除 body 超限返回 413 外，其余都是客户端输入问题，统一 400：
// 校验失败展开成字段级数组，其余（如 JSON 语法错误）只给错误键，
// 避免把 json.SyntaxError 之类的内部细节暴露给调用方。
func FailBindError(c *gin.Context, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		Fail(c, http.StatusRequestEntityTooLarge, KeyBodyTooLarge)
		return
	}

	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		fields := make([]FieldError, 0, len(validationErrs))
		for _, fe := range validationErrs {
			fields = append(fields, FieldError{
				Field: fe.Field(),
				Rule:  fe.Tag(),
				Param: fe.Param(),
			})
		}
		FailValidation(c, fields)
		return
	}

	Fail(c, http.StatusBadRequest, KeyMalformedBody)
}

// RegisterJSONFieldNames 让校验错误里的字段名用 json tag（name）而不是
// Go 字段名（Name）。gin 的校验器是全局单例，启动时调用即可，重复调用无副作用。
func RegisterJSONFieldNames() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name, _, _ := strings.Cut(fld.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			return fld.Name
		}
		return name
	})
}
