package windowscollector

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

type processInfo struct {
	name string
	path string
}

func lookupProcessInfo(pid uint32) processInfo {
	if pid == 0 {
		return processInfo{}
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return processInfo{}
	}
	defer windows.CloseHandle(handle)

	buffer := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buffer))
	err = windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size)
	if err != nil || size == 0 {
		return processInfo{}
	}

	path := windows.UTF16ToString(buffer[:size])
	name := filepath.Base(path)
	return processInfo{name: strings.TrimSpace(name), path: strings.TrimSpace(path)}
}

func processMetadata(info processInfo) map[string]string {
	metadata := map[string]string{}
	if info.name != "" {
		metadata["process_name"] = info.name
	}
	if info.path != "" {
		metadata["process_path"] = info.path
	}
	return metadata
}
