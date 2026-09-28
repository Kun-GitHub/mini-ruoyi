//go:build windows

package system

import (
	"syscall"
	"unsafe"
)

// Windows 没有 Statfs。kernel32 的 GetDiskFreeSpaceExW 是标准库之外唯一的选择，
// 但它是系统 DLL，不需要引入任何依赖。
var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// diskSpace 返回 (总量, 调用者可用, 总空闲)，单位字节。
//
// 与 Unix 版语义对齐：「可用」取的是 FreeBytesAvailableToCaller，
// 它已经扣掉了配额限制，对应 Unix 的 Bavail。
func diskSpace(path string) (total, avail, free uint64, ok bool) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, 0, false
	}

	r, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&avail)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)),
	)
	if r == 0 {
		return 0, 0, 0, false
	}
	return total, avail, free, true
}
