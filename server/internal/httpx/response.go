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

// Fail 用 i18n 键而非文案写失败响应。
func Fail(c *gin.Context, httpStatus int, msgKey string) {
	c.JSON(httpStatus, Response{Code: 1, Msg: msgKey})
}

// FailValidation 写参数校验失败响应，msg 固定为 KeyValidationFailed，
// 细节在 errors 数组里。
func FailValidation(c *gin.Context, fields []FieldError) {
	c.JSON(http.StatusBadRequest, Response{
		Code:   1,
		Msg:    KeyValidationFailed,
		Errors: fields,
	})
}

// FailFromError 把领域错误映射成 HTTP 状态码。
//
// 5xx 必须落日志：客户端只会拿到一个错误键，排查只能靠服务端日志。
func FailFromError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		Fail(c, http.StatusNotFound, KeyNotFound)
	default:
		log.Printf("%s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		Fail(c, http.StatusInternalServerError, KeyInternal)
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
