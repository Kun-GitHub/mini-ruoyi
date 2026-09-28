// Package perm 是权限码的唯一真源。
//
// 权限码声明在路由注册处（见 httpserver），本包提供：
//   - 全部权限码常量
//   - 权限清单（给授权界面用，按资源分组）
//   - 启动校验用的差异检查
//
// 显示文案不在这里：前端按约定从 i18n 字典取键。
// 权限码 system:user:add 对应的文案键是 perm.system.user.add，
// 所属分组的标题键是 perm.group.system.user。
package perm

import (
	"fmt"
	"sort"
	"strings"
)

type Code string

const (
	SystemUserList     Code = "system:user:list"
	SystemUserAdd      Code = "system:user:add"
	SystemUserEdit     Code = "system:user:edit"
	SystemUserDelete   Code = "system:user:delete"
	SystemUserResetPwd Code = "system:user:resetPwd"

	SystemRoleList   Code = "system:role:list"
	SystemRoleAdd    Code = "system:role:add"
	SystemRoleEdit   Code = "system:role:edit"
	SystemRoleDelete Code = "system:role:delete"

	SystemMenuList   Code = "system:menu:list"
	SystemMenuAdd    Code = "system:menu:add"
	SystemMenuEdit   Code = "system:menu:edit"
	SystemMenuDelete Code = "system:menu:delete"

	// SystemPermList 用于授权界面拉取权限清单本身。
	SystemPermList Code = "system:perm:list"

	// 以下属于「系统监控」。与 system:* 分开是因为它们的受众不同：
	// system:* 是改配置，monitor:* 是看运行状况，后者通常给更宽的人看。
	// 以下属于「系统工具」：主动去操作点什么，而不是看运行状况。
	ToolJobList    Code = "tool:job:list"
	ToolJobEdit    Code = "tool:job:edit"
	ToolJobRun     Code = "tool:job:run"
	ToolFileList   Code = "tool:file:list"
	ToolFileUpload Code = "tool:file:upload"
	ToolFileDelete Code = "tool:file:delete"

	MonitorSystemList   Code = "monitor:system:list"
	MonitorSessionList  Code = "monitor:session:list"
	MonitorSessionKick  Code = "monitor:session:kick"
	MonitorLoginLogList Code = "monitor:loginlog:list"
	MonitorOperLogList  Code = "monitor:operlog:list"
)

// all 是全部权限码。新增权限码必须加到这里，否则启动校验会把它当成未知码。
var all = []Code{
	SystemUserList, SystemUserAdd, SystemUserEdit, SystemUserDelete, SystemUserResetPwd,
	SystemRoleList, SystemRoleAdd, SystemRoleEdit, SystemRoleDelete,
	SystemMenuList, SystemMenuAdd, SystemMenuEdit, SystemMenuDelete,
	SystemPermList,

	ToolJobList, ToolJobEdit, ToolJobRun,
	ToolFileList, ToolFileUpload, ToolFileDelete,

	MonitorSystemList,
	MonitorSessionList, MonitorSessionKick,
	MonitorLoginLogList, MonitorOperLogList,
}

// Group 是权限清单里的一个分组，给授权界面渲染复选树用。
type Group struct {
	// TitleKey 是分组标题的 i18n 键，如 perm.group.system.user。
	TitleKey string
	Codes    []Code
}

// Groups 按「资源」把权限码分组，顺序稳定。
//
// 分组依据是权限码的前两段（system:user → system.user），
// 所以新增权限码时只要遵守 system:<资源>:<动作> 的命名，分组会自动正确。
func Groups() []Group {
	byGroup := map[string][]Code{}
	for _, c := range all {
		byGroup[groupOf(c)] = append(byGroup[groupOf(c)], c)
	}

	keys := make([]string, 0, len(byGroup))
	for k := range byGroup {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	groups := make([]Group, 0, len(keys))
	for _, k := range keys {
		codes := byGroup[k]
		sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
		groups = append(groups, Group{TitleKey: "perm.group." + k, Codes: codes})
	}
	return groups
}

// All 返回全部权限码的副本。
func All() []Code {
	out := make([]Code, len(all))
	copy(out, all)
	return out
}

// Declared 判断权限码是否已在代码里声明。
//
// 路由注册时会用它做断言：写了不存在的权限码属于编程错误，应当启动即崩，
// 而不是上线后表现为「所有人都没权限」或「这个端点没人能访问」。
func Declared(c Code) bool {
	for _, d := range all {
		if d == c {
			return true
		}
	}
	return false
}

// LabelKey 返回权限码对应的文案键：system:user:add → perm.system.user.add
func LabelKey(c Code) string {
	return "perm." + strings.ReplaceAll(string(c), ":", ".")
}

// groupOf 取权限码的前两段：system:user:add → system.user
func groupOf(c Code) string {
	parts := strings.Split(string(c), ":")
	if len(parts) < 2 {
		return string(c)
	}
	return parts[0] + "." + parts[1]
}

// Unknown 返回 codes 中不在声明表里的权限码。
//
// 用途是启动校验：sys_role_perms 表里出现未知权限码，说明代码改过名或有人直接改了库。
// 这种情况必须让进程起不来——静默失败的后果是「有人少了权限」或更糟「权限码被复用后
// 有人多了权限」，两者都极难排查。
func Unknown(codes []string) []string {
	known := make(map[string]bool, len(all))
	for _, c := range all {
		known[string(c)] = true
	}

	var unknown []string
	for _, c := range codes {
		if !known[c] {
			unknown = append(unknown, c)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// Validate 校验权限码集合，全部已知时返回 nil。
func Validate(codes []string) error {
	if unknown := Unknown(codes); len(unknown) > 0 {
		return fmt.Errorf("数据库中出现了代码未声明的权限码: %s（可能是权限码改名后未同步，或有人直接改了库）",
			strings.Join(unknown, ", "))
	}
	return nil
}
