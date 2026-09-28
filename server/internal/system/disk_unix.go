//go:build !windows

package system

import "syscall"

// diskSpace 返回 (总量, 调用者可用, 总空闲)，单位字节。
//
// 用 Bavail（非 root 可用）而不是 Bfree 作为「可用」：
// ext4 默认给 root 保留 5%，照 Bfree 算会报出用户实际上写不进去的空间。
func diskSpace(path string) (total, avail, free uint64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, false
	}
	// 字段类型各平台不一致（Linux 的 Bsize 是 int64、macOS 是 uint32），统一转换
	blockSize := uint64(st.Bsize)
	return uint64(st.Blocks) * blockSize,
		uint64(st.Bavail) * blockSize,
		uint64(st.Bfree) * blockSize,
		true
}
