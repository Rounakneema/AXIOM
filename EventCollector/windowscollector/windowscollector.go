package windowscollector

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"axiom/pkg/event"
	"axiom/pkg/goals"

	"golang.org/x/sys/windows"
)

const idleThreshold = 2 * time.Minute

type Config struct {
	PollInterval time.Duration
	Goals        *goals.Goals
	EventChan    chan<- event.Event // Write-only channel injected from main orchestrator
}

type session struct {
	hwnd       uintptr
	pid        uint32
	title      string
	activity   string
	confidence string
	idleFor    time.Duration
	process    processInfo
	startTime  time.Time
}

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

type nativeProcs struct {
	getForegroundWindow      *windows.LazyProc
	getWindowThreadProcessID *windows.LazyProc
	getWindowTextW           *windows.LazyProc
	getLastInputInfo         *windows.LazyProc
	getTickCount             *windows.LazyProc
	enumWindows              *windows.LazyProc
	isWindowVisible          *windows.LazyProc
	isIconic                 *windows.LazyProc
}

type enumState struct {
	candidates       []session
	activeHWND       uintptr
	playingAudioPIDs map[uint32]bool
	procs            nativeProcs
}

// Single package-level callback to avoid memory leaks from dynamic allocations
var enumWindowsCallback = syscall.NewCallback(func(hwnd uintptr, state *enumState) uintptr {
	if hwnd == 0 {
		return 1
	}
	if hwnd == state.activeHWND {
		return 1
	}
	visible, _, _ := state.procs.isWindowVisible.Call(hwnd)
	if visible == 0 {
		return 1
	}
	title := windowTitle(hwnd, state.procs.getWindowTextW)
	activity, _ := classifyWindow(title, 0)
	if activity != "passive_media" {
		return 1
	}
	var pid uint32
	_, _, _ = state.procs.getWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if state.playingAudioPIDs[pid] {
		return 1
	}
	minimized, _, _ := state.procs.isIconic.Call(hwnd)
	metadataActivity := "background_passive_media"
	if minimized != 0 {
		metadataActivity = "minimized_passive_media"
	}
	state.candidates = append(state.candidates, session{
		hwnd:       hwnd,
		pid:        pid,
		title:      title,
		activity:   metadataActivity,
		confidence: "low",
		process:    lookupProcessInfo(pid),
		startTime:  time.Now(),
	})
	return 1
})

func Run(ctx context.Context, cfg Config) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := windows.CoInitializeEx(0, coinitMultithreaded); err != nil {
		return err
	}
	defer windows.CoUninitialize()

	user32 := windows.NewLazySystemDLL("user32.dll")
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	procs := nativeProcs{
		getForegroundWindow:      user32.NewProc("GetForegroundWindow"),
		getWindowThreadProcessID: user32.NewProc("GetWindowThreadProcessId"),
		getWindowTextW:           user32.NewProc("GetWindowTextW"),
		getLastInputInfo:         user32.NewProc("GetLastInputInfo"),
		getTickCount:             kernel32.NewProc("GetTickCount"),
		enumWindows:              user32.NewProc("EnumWindows"),
		isWindowVisible:          user32.NewProc("IsWindowVisible"),
		isIconic:                 user32.NewProc("IsIconic"),
	}

	var prev session
	var hasPrev bool
	backgroundMedia := map[uintptr]session{}
	audioSessions := map[uint32]session{}

	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}

	for {
		select {
		case <-ctx.Done():
			now := time.Now()
			if hasPrev {
				writeSession(cfg.Goals, cfg.EventChan, prev, now, "foreground_session")
			}
			closeBackgroundMedia(cfg.Goals, cfg.EventChan, backgroundMedia, now)
			closeAudioSessions(cfg.Goals, cfg.EventChan, audioSessions, now)
			return nil
		default:
		}

		hwnd, _, _ := procs.getForegroundWindow.Call()
		if hwnd == 0 {
			time.Sleep(cfg.PollInterval)
			continue
		}

		current, err := snapshotWindow(hwnd, procs)
		if err != nil {
			return err
		}

		playingAudioPIDs, err := updateAudioSessions(cfg.Goals, cfg.EventChan, audioSessions)
		if err != nil {
			fmt.Println("audio session scan skipped:", err)
		}
		if err := updateBackgroundMedia(cfg.Goals, cfg.EventChan, hwnd, backgroundMedia, playingAudioPIDs, procs); err != nil {
			return err
		}

		if current.activity == "system_ui" {
			time.Sleep(cfg.PollInterval)
			continue
		}

		if !hasPrev {
			prev = current
			hasPrev = true
			printStart("Foreground Start", current)
		} else if prev.hwnd != current.hwnd || prev.activity != current.activity {
			endTime := time.Now()
			writeSession(cfg.Goals, cfg.EventChan, prev, endTime, "foreground_session")
			fmt.Printf("end=%s duration=%s hwnd=%d pid=%d process=%q title=%q activity=%s confidence=%s idle_for=%s\n", endTime.Format(time.RFC3339), endTime.Sub(prev.startTime), prev.hwnd, prev.pid, prev.process.name, prev.title, prev.activity, prev.confidence, prev.idleFor)

			prev = current
			printStart("Foreground Start", current)
		}

		time.Sleep(cfg.PollInterval)
	}
}

func snapshotWindow(hwnd uintptr, procs nativeProcs) (session, error) {
	var pid uint32
	_, _, _ = procs.getWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))

	title := windowTitle(hwnd, procs.getWindowTextW)
	idleFor := getIdleDuration(procs.getLastInputInfo, procs.getTickCount)
	activity, confidence := classifyWindow(title, idleFor)

	return session{
		hwnd:       hwnd,
		pid:        pid,
		title:      title,
		activity:   activity,
		confidence: confidence,
		idleFor:    idleFor,
		process:    lookupProcessInfo(pid),
		startTime:  time.Now(),
	}, nil
}

func windowTitle(hwnd uintptr, procGetWindowTextW *windows.LazyProc) string {
	buffer := make([]uint16, 256)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 {
		return ""
	}
	return string(utf16.Decode(buffer[:n]))
}

func getIdleDuration(procGetLastInputInfo, procGetTickCount *windows.LazyProc) time.Duration {
	info := lastInputInfo{cbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	ok, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return 0
	}
	tickCount, _, _ := procGetTickCount.Call()
	idleMillis := uint32(tickCount) - info.dwTime
	return time.Duration(idleMillis) * time.Millisecond
}

func updateAudioSessions(g *goals.Goals, ch chan<- event.Event, active map[uint32]session) (map[uint32]bool, error) {
	now := time.Now()
	playing, err := activeAudioSessionPIDs()
	if err != nil {
		return nil, err
	}
	for pid := range playing {
		if _, exists := active[pid]; exists {
			continue
		}
		s := session{pid: pid, activity: "audio_playing", confidence: "high", process: lookupProcessInfo(pid), startTime: now}
		active[pid] = s
		printStart("Audio Session Start", s)
	}
	for pid, previous := range active {
		if playing[pid] {
			continue
		}
		writeSession(g, ch, previous, now, "audio_session")
		fmt.Printf("audio_session_end=%s duration=%s pid=%d process=%q confidence=%s\n", now.Format(time.RFC3339), now.Sub(previous.startTime), previous.pid, previous.process.name, previous.confidence)
		delete(active, pid)
	}
	return playing, nil
}

func updateBackgroundMedia(g *goals.Goals, ch chan<- event.Event, activeHWND uintptr, active map[uintptr]session, playingAudioPIDs map[uint32]bool, procs nativeProcs) error {
	now := time.Now()
	seen := map[uintptr]session{}
	windows, err := mediaCandidateWindows(activeHWND, playingAudioPIDs, procs)
	if err != nil {
		return err
	}
	for _, candidate := range windows {
		seen[candidate.hwnd] = candidate
		if _, exists := active[candidate.hwnd]; !exists {
			active[candidate.hwnd] = candidate
			printStart("Background Media Start", candidate)
		}
	}
	for hwnd, previous := range active {
		if _, exists := seen[hwnd]; exists {
			continue
		}
		writeSession(g, ch, previous, now, "background_media_session")
		fmt.Printf("background_media_end=%s duration=%s hwnd=%d pid=%d process=%q title=%q confidence=%s\n", now.Format(time.RFC3339), now.Sub(previous.startTime), previous.hwnd, previous.pid, previous.process.name, previous.title, previous.confidence)
		delete(active, hwnd)
	}
	return nil
}

func mediaCandidateWindows(activeHWND uintptr, playingAudioPIDs map[uint32]bool, procs nativeProcs) ([]session, error) {
	state := enumState{
		candidates:       nil,
		activeHWND:       activeHWND,
		playingAudioPIDs: playingAudioPIDs,
		procs:            procs,
	}

	ok, _, err := procs.enumWindows.Call(enumWindowsCallback, uintptr(unsafe.Pointer(&state)))
	if ok == 0 {
		return nil, err
	}
	return state.candidates, nil
}

func closeBackgroundMedia(g *goals.Goals, ch chan<- event.Event, active map[uintptr]session, end time.Time) {
	for hwnd, s := range active {
		writeSession(g, ch, s, end, "background_media_session")
		delete(active, hwnd)
	}
}

func closeAudioSessions(g *goals.Goals, ch chan<- event.Event, active map[uint32]session, end time.Time) {
	for pid, s := range active {
		writeSession(g, ch, s, end, "audio_session")
		delete(active, pid)
	}
}

func classifyWindow(title string, idleFor time.Duration) (string, string) {
	clean := strings.ToLower(strings.TrimSpace(title))
	if clean == "" {
		if idleFor >= idleThreshold {
			return "idle_open", "low"
		}
		return "unknown", "low"
	}
	if strings.Contains(clean, "snap assist") {
		return "system_ui", "high"
	}
	if strings.Contains(clean, "apple music") || strings.Contains(clean, "spotify") || strings.Contains(clean, "youtube") || strings.Contains(clean, "music") || strings.Contains(clean, "playing") {
		return "passive_media", "medium"
	}
	if idleFor >= idleThreshold {
		return "idle_open", "medium"
	}
	return "foreground_interactive", "medium"
}

func writeSession(g *goals.Goals, ch chan<- event.Event, s session, end time.Time, eventType string) {
	if ch == nil {
		return
	}

	metadata := processMetadata(s.process)
	metadata["idle_for_ms"] = strconv.FormatInt(s.idleFor.Milliseconds(), 10)

	// Build default Event structure
	payload := event.Event{
		Type:       eventType,
		Source:     "windowscollector",
		App:        s.process.name,
		Activity:   s.activity,
		Confidence: s.confidence,
		Time:       s.startTime,
		EndTime:    end,
		Duration:   end.Sub(s.startTime),
		HWND:       s.hwnd,
		PID:        s.pid,
		Title:      s.title,
		Metadata:   metadata,
	}

	lowerName := strings.ToLower(s.process.name)
	isBrowser := strings.Contains(lowerName, "chrome") || strings.Contains(lowerName, "edge") || strings.Contains(lowerName, "firefox") || strings.Contains(lowerName, "brave") || strings.Contains(lowerName, "safari")

	// 🛑 FALSE-POSITIVE PROOFING 🛑
	// AXIOM's Chrome extension provides 100% accurate URL-based session events via the local API.
	// We skip logging Windows title-based sessions for browsers to prevent duplicates and false positives.
	// We still log audio sessions or other event types from browsers if needed.
	if isBrowser && eventType == "foreground_session" {
		return
	}

	// Real-time Tagging Engine using goals.yaml rules
	if g != nil {
		// Category Tagging
		cat := g.ClassifyApp(s.process.name)
		
		// If it's a known browser, the title context overrides the application category
		isBrowser := false
		lowerName := strings.ToLower(s.process.name)
		if strings.Contains(lowerName, "chrome") || strings.Contains(lowerName, "edge") || strings.Contains(lowerName, "firefox") || strings.Contains(lowerName, "brave") || strings.Contains(lowerName, "safari") {
			isBrowser = true
		}

		if isBrowser {
			// Extract domain for metadata if possible (naive approach)
			parts := strings.Fields(strings.ToLower(s.title))
			for _, part := range parts {
				if strings.Contains(part, ".") {
					payload.Domain = part
					break
				}
			}
			// Fallback extract common domains from title without dot
			if payload.Domain == "" {
				if strings.Contains(strings.ToLower(s.title), "youtube") {
					payload.Domain = "youtube.com"
				} else if strings.Contains(strings.ToLower(s.title), "github") {
					payload.Domain = "github.com"
				} else if strings.Contains(strings.ToLower(s.title), "stack overflow") {
					payload.Domain = "stackoverflow.com"
				}
			}

			// Classify by the extracted domain first, then fallback to title
			domainCat := "unknown"
			if payload.Domain != "" {
				domainCat = g.ClassifyDomain(payload.Domain)
			}
			if domainCat == "unknown" {
				domainCat = g.ClassifyDomain(s.title)
			}

			// --- Smart YouTube Heuristics ---
			if payload.Domain == "youtube.com" {
				lowerTitle := strings.ToLower(s.title)
				if strings.Contains(lowerTitle, "music") || strings.Contains(lowerTitle, "lofi") || strings.Contains(lowerTitle, "song") || strings.Contains(lowerTitle, "lyrical") || strings.Contains(lowerTitle, "visualiser") || strings.Contains(lowerTitle, "album") || strings.Contains(lowerTitle, "spotify") {
					domainCat = "music"
				} else if strings.Contains(lowerTitle, "tutorial") || strings.Contains(lowerTitle, "course") || strings.Contains(lowerTitle, "lecture") || strings.Contains(lowerTitle, "aws") || strings.Contains(lowerTitle, "kubernetes") || strings.Contains(lowerTitle, "golang") || strings.Contains(lowerTitle, "devsecops") || strings.Contains(lowerTitle, "learn") {
					domainCat = "learning"
				} else {
					domainCat = "entertainment"
				}
			}
			// ---------------------------------

			if domainCat != "unknown" {
				cat = domainCat
			} else {
				cat = "entertainment" // Default unclassified browser activity to entertainment
			}
		}

		if cat != "unknown" {
			payload.Category = cat
		} else {
			payload.Category = "unknown"
		}

		// Project Matching
		if proj, ok := g.GetProjectForPath(s.title); ok {
			payload.Project = proj
			payload.GoalRelevant = 1
		} else {
			// Title keyword matching
			for _, p := range g.Projects {
				if strings.Contains(strings.ToLower(s.title), strings.ToLower(p.Name)) {
					payload.Project = p.Name
					payload.GoalRelevant = 1
					break
				}
			}
		}

		// Direct Learning/Goals relevance matching
		if payload.GoalRelevant == 0 && (payload.Category == "coding" || payload.Category == "learning") {
			payload.GoalRelevant = 1
		}
	}

	// Bounded block on the channel to handle transient SQLite locks
	select {
	case ch <- payload:
	case <-time.After(100 * time.Millisecond):
		fmt.Printf("⚠️ Warning: DB Queue channel full for 100ms, dropped event: %s\n", eventType)
	}
}

func printStart(label string, s session) {
	fmt.Printf("%s hwnd=%d pid=%d process=%q title=%q activity=%s confidence=%s idle_for=%s at %s\n", label, s.hwnd, s.pid, s.process.name, s.title, s.activity, s.confidence, s.idleFor, s.startTime.Format(time.RFC3339))
}
