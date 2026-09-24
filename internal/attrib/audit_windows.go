//go:build windows

package attrib

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// securityChannel is where file-system auditing reports object access.
const securityChannel = "Security"

// securityQuery asks the event log for future object-access events only. The
// audit ACE keeps that stream to writes under the monitored directories.
const securityQuery = `*[System[(EventID=4663)]]`

// Values from winevt.h. Getting the delivery action wrong would drop every
// event silently, so the callback tests pin them.
const (
	evtSubscribeToFutureEvents = 1
	evtSubscribeActionError    = 0
	evtSubscribeActionDeliver  = 1
	evtRenderEventXML          = 1
)

// auditSource reads the writer of a file write from Windows file-system
// auditing: an audit ACE on each monitored directory makes the Security log
// report every access to it, with the accessing process in the event.
type auditSource struct {
	paths  []string
	setup  bool
	log    *slog.Logger
	parent *devicePaths

	// deliver is set before the subscription starts; the event log delivers
	// nothing until EvtSubscribe returns.
	deliver func(Writer)

	subscription uintptr
	target       uintptr
	started      bool
	closeOnce    sync.Once

	records atomic.Uint64
	skipped atomic.Uint64
	errors  atomic.Uint64
}

func newAuditSource(paths []string, setup bool, log *slog.Logger) *auditSource {
	return &auditSource{paths: paths, setup: setup, log: log}
}

func (s *auditSource) Describe() string {
	return "Windows file-system auditing (Security event 4663)"
}

// Start enables auditing for the monitored directories and subscribes to the
// Security log. It fails rather than starting blind: a source that cannot report
// writers must not look like a quiet host.
func (s *auditSource) Start(record func(Writer)) error {
	if !isElevated() {
		return fmt.Errorf("file-system auditing needs an elevated process, and this one is not elevated")
	}

	if s.setup {
		if err := enableFileSystemAuditing(); err != nil {
			return fmt.Errorf("enable file-system auditing: %w", err)
		}
		if err := s.auditPaths(); err != nil {
			return err
		}
	}

	if !s.setup {
		// The operator owns the policy in this case, so say so when the
		// subcategory is off rather than delivering nothing mysteriously.
		if policy, err := systemAuditPolicy(); err == nil && policy&policyAuditSuccess == 0 {
			s.log.Warn("success auditing is off for the File System subcategory; no audited records will arrive",
				"subcategory", "File System")
		}
	}

	s.parent = newDevicePaths()
	s.deliver = record

	s.target = s.registerTarget()
	subscription, err := subscribeToSecurityLog(s.target)
	if err != nil {
		subscribeTargets.Delete(s.target)
		s.target = 0
		return fmt.Errorf("subscribe to the Security log: %w", err)
	}
	s.subscription = subscription
	s.started = true

	s.log.Info("auditing file-system writes", "channel", securityChannel, "paths", len(s.paths))
	return nil
}

// auditPaths puts an audit ACE on every monitored directory. One unusable
// directory costs coverage there, not everywhere, so only losing all of them is
// a reason to give up on the mechanism.
func (s *auditSource) auditPaths() error {
	var firstErr error
	audited := 0

	for _, path := range s.paths {
		if err := setAuditACE(path); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.log.Warn("audit ACE not set", "path", path, "error", err)
			continue
		}
		audited++
	}

	if audited == 0 {
		return fmt.Errorf("no monitored directory could be audited: %w", firstErr)
	}
	return nil
}

func (s *auditSource) Stats() SourceStats {
	return SourceStats{
		Records: s.records.Load(),
		Skipped: s.skipped.Load(),
		Errors:  s.errors.Load(),
	}
}

// Close stops delivery. It leaves the audit ACE and the audit policy in place:
// they are deployment configuration, and removing them would undo an operator's
// own settings.
func (s *auditSource) Close() error {
	s.closeOnce.Do(func() {
		if s.subscription != 0 {
			procEvtClose.Call(s.subscription)
			s.subscription = 0
		}
		if s.target != 0 {
			subscribeTargets.Delete(s.target)
			s.target = 0
		}
		if !s.started {
			return
		}
		s.log.Info("audit source stopped",
			"records", s.records.Load(),
			"skipped", s.skipped.Load(),
			"errors", s.errors.Load())
	})
	return nil
}

// handleEvent parses one rendered event and files the writer it names.
func (s *auditSource) handleEvent(xmlText string) {
	writer, ok, err := parseAuditEvent(xmlText, s.parent)
	if err != nil {
		s.errors.Add(1)
		return
	}
	if !ok {
		s.skipped.Add(1)
		return
	}
	s.records.Add(1)
	s.deliver(writer)
}

var (
	modwevtapi       = windows.NewLazySystemDLL("wevtapi.dll")
	procEvtSubscribe = modwevtapi.NewProc("EvtSubscribe")
	procEvtRender    = modwevtapi.NewProc("EvtRender")
	procEvtClose     = modwevtapi.NewProc("EvtClose")
)

// evtSubscribeCallback is the delivery callback the event log invokes.
var evtSubscribeCallback = syscall.NewCallback(handleSubscribeNotify)

// The subscription context is a numeric token, not a Go pointer: handing a
// pointer to the event log would let it outlive what it points at.
var (
	subscribeTargets sync.Map // token -> *auditSource
	subscribeTokens  atomic.Uintptr
)

func (s *auditSource) registerTarget() uintptr {
	token := subscribeTokens.Add(1)
	subscribeTargets.Store(token, s)
	return token
}

func targetFor(token uintptr) *auditSource {
	source, ok := subscribeTargets.Load(token)
	if !ok {
		return nil
	}
	return source.(*auditSource)
}

// handleSubscribeNotify renders and files one delivered event. It runs on an
// event-log thread and must not panic across the syscall boundary, so every
// failure is counted instead of raised.
func handleSubscribeNotify(action uint32, context uintptr, event uintptr) uintptr {
	source := targetFor(context)
	if source == nil {
		return 0
	}

	switch action {
	case evtSubscribeActionDeliver:
		if event != 0 {
			deliverRenderedEvent(source, event)
		}
	case evtSubscribeActionError:
		// The event handle carries the Win32 error code in this case.
		if source.errors.Add(1) == 1 {
			source.log.Warn("event log subscription error", "code", uint32(event))
		}
	}
	return 0
}

func deliverRenderedEvent(source *auditSource, event uintptr) {
	defer func() {
		if r := recover(); r != nil {
			source.errors.Add(1)
		}
	}()

	xmlText, err := renderEventXML(event)
	if err != nil {
		source.errors.Add(1)
		return
	}
	source.handleEvent(xmlText)
}

// subscribeToSecurityLog opens a push subscription, so events arrive as the log
// writes them rather than on a poll interval.
func subscribeToSecurityLog(context uintptr) (uintptr, error) {
	channel, err := windows.UTF16PtrFromString(securityChannel)
	if err != nil {
		return 0, err
	}
	query, err := windows.UTF16PtrFromString(securityQuery)
	if err != nil {
		return 0, err
	}

	handle, _, callErr := procEvtSubscribe.Call(
		0, 0,
		uintptr(unsafe.Pointer(channel)),
		uintptr(unsafe.Pointer(query)),
		0,
		context,
		evtSubscribeCallback,
		evtSubscribeToFutureEvents,
	)
	if handle == 0 {
		return 0, syscallError(callErr, "EvtSubscribe")
	}
	return handle, nil
}

// renderEventXML asks the event log for one event's XML.
func renderEventXML(event uintptr) (string, error) {
	var used, properties uint32

	procEvtRender.Call(0, event, evtRenderEventXML, 0, 0,
		uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&properties)))
	if used == 0 {
		return "", fmt.Errorf("EvtRender reported no size")
	}

	buf := make([]uint16, used/2+1)
	ok, _, callErr := procEvtRender.Call(0, event, evtRenderEventXML,
		uintptr(len(buf)*2), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&properties)))
	if ok == 0 {
		return "", syscallError(callErr, "EvtRender")
	}
	return windows.UTF16ToString(buf), nil
}

// syscallError turns a lazy-proc error into something worth logging.
func syscallError(err error, call string) error {
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("%s failed", call)
	}
	return fmt.Errorf("%s: %w", call, err)
}

// enableFileSystemAuditing turns on success auditing for the File System
// subcategory, keeping whatever the host already audits for that subcategory.
func enableFileSystemAuditing() error {
	current, err := systemAuditPolicy()
	if err != nil {
		return err
	}
	want := auditPolicyWithSuccess(current)
	if want == current {
		return nil
	}
	return setSystemAuditPolicy(want)
}
