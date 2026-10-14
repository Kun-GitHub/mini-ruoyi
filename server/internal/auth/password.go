// 有问题欢迎联系 hot_kun@hotmail.com
// Package auth 负责密码校验与会话管理。
package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"

	"mini-ruoyi/internal/domain"
)

// MaxPasswordBytes 是 bcrypt 的上限。
//
// bcrypt 对超过 72 **字节** 的密码直接报错，不会截断。但请求校验里的
// binding:"max=72" 数的是**字符**（rune），前端 maxlength 数的是 UTF-16 码元，
// 两者都拦不住非 ASCII 的长密码——25 个汉字是 25 个字符、75 个字节。
//
// 所以 HashPassword 会把 bcrypt 的这个错误换成 domain.ErrPasswordTooLong，
// 让建号、重置密码、改自己密码、批量导入四条路径统一回 400，而不是 500。
const MaxPasswordBytes = 72

// HashPassword 生成 bcrypt 哈希（60 字符）。
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		if errors.Is(err, bcrypt.ErrPasswordTooLong) {
			return "", domain.ErrPasswordTooLong
		}
		return "", err
	}
	return string(b), nil
}

// VerifyPassword 校验明文与哈希是否匹配。
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// dummyHash 用于「用户名不存在」时也走一次 bcrypt 比对。
//
// 不这么做的话，用户名不存在会立刻返回，而密码错误要等一次 bcrypt 运算（几十毫秒），
// 攻击者据此就能枚举出哪些用户名是有效的。代价是启动时多一次哈希运算。
var dummyHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-constant-time"), bcrypt.DefaultCost)
	if err != nil {
		panic(err) // 只可能在 cost 非法时发生，属于编码错误
	}
	return h
}()
