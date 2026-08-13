package windowscollector

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clsctxAll             = 23
	coinitMultithreaded   = 0
	audioSessionStateLive = 1
)

var (
	clsidMMDeviceEnumerator  = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator   = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioSessionManager2 = windows.GUID{Data1: 0x77AA99A0, Data2: 0x1BD6, Data3: 0x484F, Data4: [8]byte{0x8B, 0xC7, 0x2C, 0x65, 0x4C, 0x9A, 0x9B, 0x6F}}
	iidIAudioSessionControl2 = windows.GUID{Data1: 0xBFB7FF88, Data2: 0x7239, Data3: 0x4FC9, Data4: [8]byte{0x8F, 0xA2, 0x07, 0xC9, 0x50, 0xBE, 0x9C, 0x6D}}

	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

type comObject struct {
	lpVtbl unsafe.Pointer
}

type iUnknownVtbl struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr
}

type mmDeviceEnumeratorVtbl struct {
	iUnknownVtbl
	enumAudioEndpoints      uintptr
	getDefaultAudioEndpoint uintptr
}

type mmDeviceVtbl struct {
	iUnknownVtbl
	activate uintptr
}

type audioSessionManager2Vtbl struct {
	iUnknownVtbl
	getAudioSessionControl uintptr
	getSimpleAudioVolume   uintptr
	getSessionEnumerator   uintptr
}

type audioSessionEnumeratorVtbl struct {
	iUnknownVtbl
	getCount   uintptr
	getSession uintptr
}

type audioSessionControlVtbl struct {
	iUnknownVtbl
	getState                    uintptr
	getDisplayName              uintptr
	setDisplayName              uintptr
	getIconPath                 uintptr
	setIconPath                 uintptr
	getGroupingParam            uintptr
	setGroupingParam            uintptr
	registerAudioSessionNotif   uintptr
	unregisterAudioSessionNotif uintptr
}

type audioSessionControl2Vtbl struct {
	audioSessionControlVtbl
	getSessionIdentifier         uintptr
	getSessionInstanceIdentifier uintptr
	getProcessID                 uintptr
}

func activeAudioSessionPIDs() (map[uint32]bool, error) {
	deviceEnumerator, err := createMMDeviceEnumerator()
	if err != nil {
		return nil, err
	}
	defer release(deviceEnumerator)

	device, err := defaultRenderDevice(deviceEnumerator)
	if err != nil {
		return nil, err
	}
	defer release(device)

	manager, err := audioSessionManager(device)
	if err != nil {
		return nil, err
	}
	defer release(manager)

	enumerator, err := audioSessionEnumerator(manager)
	if err != nil {
		return nil, err
	}
	defer release(enumerator)

	count, err := audioSessionCount(enumerator)
	if err != nil {
		return nil, err
	}

	active := map[uint32]bool{}
	for i := 0; i < count; i++ {
		control, err := audioSessionAt(enumerator, i)
		if err != nil {
			continue
		}

		state, stateErr := audioSessionState(control)
		control2, queryErr := audioSessionControl2(control)
		release(control)
		if stateErr != nil || queryErr != nil {
			continue
		}

		pid, pidErr := audioSessionProcessID(control2)
		release(control2)
		if pidErr != nil || pid == 0 || state != audioSessionStateLive {
			continue
		}
		active[pid] = true
	}

	return active, nil
}

func createMMDeviceEnumerator() (*comObject, error) {
	var obj *comObject
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0,
		uintptr(clsctxAll),
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&obj)),
	)
	if failed(hr) {
		return nil, fmt.Errorf("CoCreateInstance IMMDeviceEnumerator failed: 0x%x", hr)
	}
	return obj, nil
}

func defaultRenderDevice(enumerator *comObject) (*comObject, error) {
	vtbl := (*mmDeviceEnumeratorVtbl)(enumerator.lpVtbl)
	var device *comObject
	hr, _, _ := syscall.SyscallN(vtbl.getDefaultAudioEndpoint, uintptr(unsafe.Pointer(enumerator)), 0, 0, uintptr(unsafe.Pointer(&device)))
	if failed(hr) {
		return nil, fmt.Errorf("GetDefaultAudioEndpoint failed: 0x%x", hr)
	}
	return device, nil
}

func audioSessionManager(device *comObject) (*comObject, error) {
	vtbl := (*mmDeviceVtbl)(device.lpVtbl)
	var manager *comObject
	hr, _, _ := syscall.SyscallN(vtbl.activate, uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(&iidIAudioSessionManager2)), uintptr(clsctxAll), 0, uintptr(unsafe.Pointer(&manager)))
	if failed(hr) {
		return nil, fmt.Errorf("IMMDevice Activate IAudioSessionManager2 failed: 0x%x", hr)
	}
	return manager, nil
}

func audioSessionEnumerator(manager *comObject) (*comObject, error) {
	vtbl := (*audioSessionManager2Vtbl)(manager.lpVtbl)
	var enumerator *comObject
	hr, _, _ := syscall.SyscallN(vtbl.getSessionEnumerator, uintptr(unsafe.Pointer(manager)), uintptr(unsafe.Pointer(&enumerator)))
	if failed(hr) {
		return nil, fmt.Errorf("GetSessionEnumerator failed: 0x%x", hr)
	}
	return enumerator, nil
}

func audioSessionCount(enumerator *comObject) (int, error) {
	vtbl := (*audioSessionEnumeratorVtbl)(enumerator.lpVtbl)
	var count int32
	hr, _, _ := syscall.SyscallN(vtbl.getCount, uintptr(unsafe.Pointer(enumerator)), uintptr(unsafe.Pointer(&count)))
	if failed(hr) {
		return 0, fmt.Errorf("GetCount failed: 0x%x", hr)
	}
	return int(count), nil
}

func audioSessionAt(enumerator *comObject, index int) (*comObject, error) {
	vtbl := (*audioSessionEnumeratorVtbl)(enumerator.lpVtbl)
	var control *comObject
	hr, _, _ := syscall.SyscallN(vtbl.getSession, uintptr(unsafe.Pointer(enumerator)), uintptr(uint32(index)), uintptr(unsafe.Pointer(&control)))
	if failed(hr) {
		return nil, fmt.Errorf("GetSession failed: 0x%x", hr)
	}
	return control, nil
}

func audioSessionState(control *comObject) (uint32, error) {
	vtbl := (*audioSessionControlVtbl)(control.lpVtbl)
	var state uint32
	hr, _, _ := syscall.SyscallN(vtbl.getState, uintptr(unsafe.Pointer(control)), uintptr(unsafe.Pointer(&state)))
	if failed(hr) {
		return 0, fmt.Errorf("GetState failed: 0x%x", hr)
	}
	return state, nil
}

func audioSessionControl2(control *comObject) (*comObject, error) {
	vtbl := (*iUnknownVtbl)(control.lpVtbl)
	var control2 *comObject
	hr, _, _ := syscall.SyscallN(vtbl.queryInterface, uintptr(unsafe.Pointer(control)), uintptr(unsafe.Pointer(&iidIAudioSessionControl2)), uintptr(unsafe.Pointer(&control2)))
	if failed(hr) {
		return nil, fmt.Errorf("QueryInterface IAudioSessionControl2 failed: 0x%x", hr)
	}
	return control2, nil
}

func audioSessionProcessID(control2 *comObject) (uint32, error) {
	vtbl := (*audioSessionControl2Vtbl)(control2.lpVtbl)
	var pid uint32
	hr, _, _ := syscall.SyscallN(vtbl.getProcessID, uintptr(unsafe.Pointer(control2)), uintptr(unsafe.Pointer(&pid)))
	if failed(hr) {
		return 0, fmt.Errorf("GetProcessId failed: 0x%x", hr)
	}
	return pid, nil
}

func release(obj *comObject) {
	if obj == nil {
		return
	}
	vtbl := (*iUnknownVtbl)(obj.lpVtbl)
	syscall.SyscallN(vtbl.release, uintptr(unsafe.Pointer(obj)))
}

func failed(hr uintptr) bool {
	return hr&0x80000000 != 0
}
