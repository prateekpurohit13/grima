package attrib

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Sources of a blame decision.
const (
	sourceCausal    = "causal"
	sourceCorrelate = "correlate"
)

// decision is one blame decision, as JSON on one line of a trace file.
type decision struct {
	Time       string  `json:"time"`
	PID        int32   `json:"pid"`
	Name       string  `json:"name"`
	Source     string  `json:"source"`
	Candidates int     `json:"candidates"`
	TopBytes   uint64  `json:"top_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
	Confidence float64 `json:"confidence"`
}

// tracePathEnv names the file that receives one line per blame decision. A
// detector run with it set can be scored against a known writer.
const tracePathEnv = "GRIMA_ATTRIB_TRACE"

// traceMaxLines bounds the trace file, so a detector left running for days
// cannot fill the disk.
const traceMaxLines = 200000

type tracer struct {
	mu       sync.Mutex
	file     *os.File
	maxLines int
	lines    int
	dropped  uint64
}

// newTracerFromEnv returns nil unless GRIMA_ATTRIB_TRACE names a file.
func newTracerFromEnv() *tracer {
	path := os.Getenv(tracePathEnv)
	if path == "" {
		return nil
	}
	return newTracer(path, traceMaxLines)
}

func newTracer(path string, maxLines int) *tracer {
	t := &tracer{maxLines: maxLines}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return t // no file: every decision is counted as dropped
	}
	t.file = file
	return t
}

// record writes one decision. A trace that cannot be written degrades the
// measurement; it never affects attribution.
func (t *tracer) record(d decision) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.file == nil || t.lines >= t.maxLines {
		t.dropped++
		return
	}
	line, err := json.Marshal(d)
	if err != nil {
		t.dropped++
		return
	}
	if _, err := t.file.Write(append(line, '\n')); err != nil {
		t.dropped++
		return
	}
	t.lines++
}

func (t *tracer) droppedLines() uint64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dropped
}

// close releases the trace file. A detector holds it for its lifetime; tests
// close it so the file is not still locked when the test cleans up.
func (t *tracer) close() error {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// A trace that silently under-counts would skew any accuracy measured from
	// it, so the loss is reported where an operator will see it.
	if t.dropped > 0 {
		fmt.Fprintf(os.Stderr, "attrib: %s trace dropped %d of %d decisions; the measurement is incomplete\n",
			tracePathEnv, t.dropped, t.dropped+uint64(t.lines))
	}

	if t.file == nil {
		return nil
	}
	err := t.file.Close()
	t.file = nil
	return err
}

// newDecision describes one Suspect call for the trace.
func newDecision(now time.Time, pid int32, name string, candidates int, top, total uint64, confidence float64, source string) decision {
	return decision{
		Time:       now.UTC().Format(time.RFC3339Nano),
		PID:        pid,
		Name:       name,
		Source:     source,
		Candidates: candidates,
		TopBytes:   top,
		TotalBytes: total,
		Confidence: confidence,
	}
}
