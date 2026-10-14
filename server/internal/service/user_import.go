// 有问题欢迎联系 hot_kun@hotmail.com
package service

import (
	"context"
	"fmt"

	"mini-ruoyi/internal/auth"
	"mini-ruoyi/internal/domain"
)

// ImportFirstDataRow 是数据行的起始行号：表头占第 1 行。
//
// 行号是**文件里的行号**，不是数组下标——用户拿着它才能回到文件定位。
// 逐行校验在 handler 层做，那里同样按这个口径算行号。
const ImportFirstDataRow = 2

// ImportRow 是导入文件里的一行，只含导入认得的列。
//
// 校验标签（binding）与 json 标签跟着数据走，而不是在 handler 里再抄一份结构体：
// 抄一份的话，两处的规则迟早会漂开，表现是「手工建号被拦下、导入却能进库」。
// 校验由 handler 调 gin 的校验器完成（字段名按 json 标签输出），service 不碰 HTTP。
type ImportRow struct {
	// 规则必须与 createUserRequest 保持一致
	Username string `json:"username" binding:"required,min=2,max=128"`
	Nickname string `json:"nickname" binding:"max=128"`
	Mobile   string `json:"mobile" binding:"max=20"`
	Email    string `json:"email" binding:"omitempty,email,max=64"`
	Status   string `json:"status" binding:"omitempty,oneof=active inactive"`
}

// ImportRowError 是一条绑在文件行号上的错误。
type ImportRowError struct {
	Row   int    `json:"row"`
	Field string `json:"field"`
	Rule  string `json:"rule"`
	Param string `json:"param,omitempty"`
}

// ImportResult 是服务端能判定的部分：用户名是否已被占用。
// 行级格式校验归 handler，两者最后在 handler 里合成一份报告。
type ImportResult struct {
	Imported int
	Errors   []ImportRowError
}

// Import 检查用户名占用并整批写入。
//
// 密码由调用方整批传入，**整批只哈希一次**：bcrypt 一个哈希约 50ms，
// 逐行哈希 500 行要 20 秒以上，会撞上 HTTP 写的 10 秒上限（cmd/server/main.go）。
// 代价是同一批用户初始密码相同——导入本来就是「先建号、再各自改密」的场景。
//
// 一行不合格就一行都不写：唯一冲突存在、或调用方自己已经判定有行级错误时
// （这时它会把 dryRun 传成 true），这里都不落库。跳过坏行继续导的话，
// 用户会拿到一份「到底进来了哪些」说不清的列表。
//
// dryRun 只查不写，报告与真导入同形，界面上的「校验」按钮走的就是它。
func (s *UserService) Import(ctx context.Context, rows []ImportRow, password string, dryRun bool) (ImportResult, error) {
	var (
		result  ImportResult
		pending = make([]domain.User, 0, len(rows))
	)

	for i, row := range rows {
		taken, err := s.repo.ExistsUsername(ctx, row.Username, 0)
		if err != nil {
			return ImportResult{}, fmt.Errorf("check username %q: %w", row.Username, err)
		}
		if taken {
			result.Errors = append(result.Errors, ImportRowError{
				Row:   i + ImportFirstDataRow,
				Field: "username",
				Rule:  "unique",
			})
			continue
		}
		pending = append(pending, domain.User{
			Status:   orActive(row.Status),
			Username: row.Username,
			Nickname: row.Nickname,
			Mobile:   row.Mobile,
			Email:    row.Email,
		})
	}

	if dryRun || len(result.Errors) > 0 {
		return result, nil
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return ImportResult{}, fmt.Errorf("hash password: %w", err)
	}
	for i := range pending {
		pending[i].Password = hash
	}
	if err := s.repo.CreateBatch(ctx, pending); err != nil {
		return ImportResult{}, fmt.Errorf("create %d users: %w", len(pending), err)
	}
	result.Imported = len(pending)
	return result, nil
}
