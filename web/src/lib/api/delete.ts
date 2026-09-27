import { ApiError, api } from './client'

import { confirm } from '$lib/stores/confirm.svelte'

/**
 * 删除资源，遇到「有子数据」时先弹确认框。
 *
 * 这条流程值得单独封装，因为它的顺序很容易写错：
 * 后端在资源有依赖时返回 409 + 影响面，**那是确认信号而不是错误**。
 * 如果调用方按常规错误处理（弹一个红色提示），用户会先看到报错、
 * 再看到确认框，完全不知道发生了什么。
 *
 * 返回值表示「是否真的删掉了」：false 代表用户在确认框里取消了。
 */
export async function deleteWithConfirm(path: string): Promise<boolean> {
  try {
    await api.del(path)
    return true
  } catch (err) {
    if (!(err instanceof ApiError) || !err.needsConfirmation || !err.dependents) {
      throw err
    }

    const ok = await confirm({
      titleKey: 'confirm.deleteTitle',
      bodyKey: 'confirm.hasDependents',
      // 影响面的字段名由后端给出（child_menus / affected_roles / affected_users），
      // 文案键按 impact.<字段名> 约定拼出来
      details: Object.entries(err.dependents).map(([field, value]) => ({
        labelKey: `impact.${field}`,
        value,
      })),
      danger: true,
    })
    if (!ok) return false

    await api.del(`${path}?cascade=true`)
    return true
  }
}
