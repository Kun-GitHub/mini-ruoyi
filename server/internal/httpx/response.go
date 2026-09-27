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
	// KeyBackendUnreachable 表示响应不是本服务的信封格式——请求被代理/网关拦下，
	// 或者后端进程根本没起来。前端据此报「无法连接后端」，而不是笼统的 error.internal。
	KeyBackendUnreachable = "error.backendUnreachable"
	// KeyFrontendDisabled 表示后端以纯 API 模式运行、前端未部署。
	// 常见于用 IDE 或 go run 启动、且没有 bin/web 的开发场景。
	KeyFrontendDisabled = "error.frontendDisabled"
	// KeyCannotKickSelf 不允许踢掉自己当前这条会话。
	KeyCannotKickSelf = "error.cannotKickSelf"
	// KeyInvalidJobCron cron 表达式无法解析。
	KeyInvalidJobCron = "error.invalidJobCron"
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
// 语义由 HTTP 状态码承载，code 只作成功/失败标志，不重复表达状态。
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

func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: 0, Msg: SuccessMsg, Data: data})
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
	c.JSON(httpStatus, Response{Code: 1, Msg: msgKey})
}

// FailValidation 写参数校验失败响应，msg 固定为 KeyValidationFailed，
// 细节在 errors 数组里。
func FailValidation(c *gin.Context, fields []FieldError) {
	c.Set(resultKeyCtx, KeyValidationFailed)
	c.JSON(http.StatusBadRequest, Response{
		Code:   1,
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
		c.JSON(http.StatusConflict, Response{Code: 1, Msg: KeyHasDependents, Data: depErr.Impact})

	case errors.As(err, &dupErr):
		// 复用字段级错误的形状，前端能把「用户名已存在」高亮到对应输入框
		c.Set(resultKeyCtx, KeyDuplicate)
		c.JSON(http.StatusConflict, Response{
			Code:   1,
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
