//go:build darwin

package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

// pluginRenameSwapFlag is RENAME_SWAP from <sys/stdio.h>, verified in the
// installed macOS SDK. It exchanges two directories in one atomic step, so a
// reclaimed MCP proxy that spawns the backend at any moment always resolves the
// plugin folder.
const pluginRenameSwapFlag = 0x00000002

var (
	pluginSwapOnce sync.Once
	pluginSwapFn   uintptr
	pluginErrnoFn  uintptr
	pluginSwapErr  error
)

func loadPluginSwapFunction() {
	pluginSwapOnce.Do(func() {
		handle, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			pluginSwapErr = fmt.Errorf("%w: %v", errPluginSwapUnsupported, err)
			return
		}
		fn, err := purego.Dlsym(handle, "renamex_np")
		if err != nil {
			pluginSwapErr = fmt.Errorf("%w: %v", errPluginSwapUnsupported, err)
			return
		}
		pluginSwapFn = fn
		// __error returns the address of the calling thread's errno. The
		// scratch swap-return-probe showed the third SyscallN return on darwin
		// is not errno, so failure diagnostics read errno from libc itself.
		errnoFn, err := purego.Dlsym(handle, "__error")
		if err != nil {
			pluginSwapErr = fmt.Errorf("%w: %v", errPluginSwapUnsupported, err)
			return
		}
		pluginErrnoFn = errnoFn
	})
}

// pluginSwapErrno reads the calling thread's errno through libc __error. The
// caller must hold the OS thread from the failed call through this read, so
// thread-local storage still refers to the thread that ran renamex_np.
func pluginSwapErrno() syscall.Errno {
	if pluginErrnoFn == 0 {
		return 0
	}
	address, _, _ := purego.SyscallN(pluginErrnoFn)
	if address == 0 {
		return 0
	}
	// __error returns a libc-owned int32*. Reinterpreting the local uintptr
	// keeps the conversion vet-safe: the pointer value comes from libc, not
	// from a uintptr that could have moved.
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&address))
	return syscall.Errno(*(*int32)(pointer))
}

// pluginSwapDirectories exchanges candidate with target. After a successful
// call, candidate holds the previous content of target, which the caller keeps
// recoverably. A directory type mismatch or a missing symbol is reported, never
// silently replaced by a non-atomic rename.
func pluginSwapDirectories(candidate, target string) error {
	loadPluginSwapFunction()
	if pluginSwapErr != nil {
		return pluginSwapErr
	}
	from := append([]byte(candidate), 0)
	to := append([]byte(target), 0)
	// Lock the thread so the failed call and the errno read see the same
	// thread-local errno slot.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r1, _, _ := purego.SyscallN(
		pluginSwapFn,
		uintptr(unsafe.Pointer(&from[0])),
		uintptr(unsafe.Pointer(&to[0])),
		uintptr(pluginRenameSwapFlag),
	)
	runtime.KeepAlive(from)
	runtime.KeepAlive(to)
	if int64(r1) == 0 {
		return nil
	}
	if errno := pluginSwapErrno(); errno != 0 {
		return fmt.Errorf("swap plugin folder: %w", errno)
	}
	return fmt.Errorf("swap plugin folder: %s", syscall.EINVAL.Error())
}
