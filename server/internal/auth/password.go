// Package auth 负责密码校验与会话管理。
package auth

import "golang.org/x/crypto/bcrypt"

// MaxPasswordBytes 是 bcrypt 的上限。
//
// bcrypt 对超过 72 字节的密码直接返回错误，不会截断。所以请求校验必须带
// binding:"max=72"，否则用户填一个长密码会拿到 500 而不是「密码太长」。
const MaxPasswordBytes = 72

// HashPassword 生成 bcrypt 哈希（60 字符）。
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
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
