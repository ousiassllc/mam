//go:build windows

package gpu

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DXGI lists the adapters through dxgi.dll (CreateDXGIFactory1, IDXGIFactory1::EnumAdapters1,
// IDXGIAdapter1::GetDesc1), called from pure Go: no cgo, no helper process. The vtable slots are those of
// dxgi.h: IUnknown 0-2, IDXGIObject 3-6, IDXGIFactory 7-11, IDXGIFactory1 12 (EnumAdapters1);
// IDXGIAdapter 7-9, IDXGIAdapter1 10 (GetDesc1). Not exercised on the Linux development host.
type DXGI struct{}

const (
	slotRelease      = 2
	slotEnumAdapters = 12
	slotGetDesc1     = 10
	dxgiErrNotFound  = 0x887A0002 // DXGI_ERROR_NOT_FOUND: the index is past the last adapter
	softwareFlag     = 2          // DXGI_ADAPTER_FLAG_SOFTWARE
	maxAdapters      = 16
)

// iidFactory1 is IID_IDXGIFactory1 {770aae78-f26f-4dba-a829-253c83d1b387}.
var iidFactory1 = windows.GUID{Data1: 0x770aae78, Data2: 0xf26f, Data3: 0x4dba, Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}

// adapterDesc1 is DXGI_ADAPTER_DESC1 on a 64-bit Windows.
type adapterDesc1 struct {
	Description           [128]uint16
	VendorID              uint32
	DeviceID              uint32
	SubSysID              uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	LUIDLow               uint32
	LUIDHigh              int32
	Flags                 uint32
}

// method is the function pointer in slot of the vtable of the COM object obj (a pointer the DLL returned,
// held as a uintptr for SyscallN).
func method(obj uintptr, slot int) uintptr {
	object := *(*unsafe.Pointer)(unsafe.Pointer(&obj))                        //nolint:gosec // COM vtable access; the COM object: its first word is the vtable
	vtbl := *(*unsafe.Pointer)(object)                                        //nolint:gosec // the vtable of a live COM object
	return *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0))))) //nolint:gosec // slot is a constant index of the interface
}

func release(obj uintptr) { _, _, _ = syscall.SyscallN(method(obj, slotRelease), obj) }

// Adapters implements Detector.
func (DXGI) Adapters(context.Context) ([]Adapter, error) {
	dll := windows.NewLazySystemDLL("dxgi.dll")
	create := dll.NewProc("CreateDXGIFactory1")
	if err := create.Find(); err != nil {
		return nil, fmt.Errorf("dxgi.dll: %w", err)
	}
	var factory uintptr
	if hr, _, _ := create.Call(uintptr(unsafe.Pointer(&iidFactory1)), uintptr(unsafe.Pointer(&factory))); hr != 0 || factory == 0 { //nolint:gosec // out-pointers of the COM call
		return nil, fmt.Errorf("CreateDXGIFactory1 failed: 0x%x", hr)
	}
	defer release(factory)
	var out []Adapter
	for i := range maxAdapters {
		var adapter uintptr
		hr, _, _ := syscall.SyscallN(method(factory, slotEnumAdapters), factory, uintptr(i), uintptr(unsafe.Pointer(&adapter))) //nolint:gosec // out-pointer of the COM call
		if uint32(hr) == dxgiErrNotFound {                                                                                      //nolint:gosec // an HRESULT is 32 bits
			break
		}
		if hr != 0 || adapter == 0 {
			return out, fmt.Errorf("EnumAdapters1(%d) failed: 0x%x", i, hr)
		}
		var d adapterDesc1
		hr, _, _ = syscall.SyscallN(method(adapter, slotGetDesc1), adapter, uintptr(unsafe.Pointer(&d))) //nolint:gosec // out-pointer of the COM call
		release(adapter)
		if hr != 0 {
			return out, fmt.Errorf("GetDesc1(%d) failed: 0x%x", i, hr)
		}
		out = append(out, Adapter{
			Name: windows.UTF16ToString(d.Description[:]), VendorID: d.VendorID, VRAMBytes: uint64(d.DedicatedVideoMemory),
			Software: d.Flags&softwareFlag != 0,
		})
	}
	return out, nil
}
