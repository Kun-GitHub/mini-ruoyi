package repository

import "strings"

// likePattern 把用户输入包成 LIKE 模式，并转义 LIKE 的通配符。
//
// 不转义的话，用户输入一个 `%` 就会匹配全部记录、输入 `_` 会把单字符当通配符——
// 不是安全问题（参数仍是绑定的，不会注入），但是功能错误：用户以为在筛选，
// 实际拿到了全部数据。
//
// 反斜杠必须最先替换，否则后续插入的转义符会被二次转义。
func likePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// likeClause 是配合 likePattern 使用的条件片段。
// SQLite 默认不支持 ESCAPE，必须显式声明，否则反斜杠会被当成普通字符。
const likeClause = ` LIKE ? ESCAPE '\'`

// whereBuilder 累积 WHERE 条件，避免 List 与 Count 各写一遍导致两者不一致
// （不一致的后果是「分页总数和实际条数对不上」这种很难查的问题）。
type whereBuilder struct {
	conds []string
	args  []any
}

func (b *whereBuilder) like(column, value string) {
	if value == "" {
		return
	}
	b.conds = append(b.conds, column+likeClause)
	b.args = append(b.args, likePattern(value))
}

func (b *whereBuilder) eq(column, value string) {
	if value == "" {
		return
	}
	b.conds = append(b.conds, column+" = ?")
	b.args = append(b.args, value)
}

// clause 返回可直接拼在 FROM 之后的片段；没有任何条件时返回空串。
func (b *whereBuilder) clause() string {
	if len(b.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(b.conds, " AND ")
}
