package score

import (
	"fmt"
	"sort"

	"github.com/prateekpurohit13/grima/internal/calibrate"
	"github.com/prateekpurohit13/grima/internal/fingerprint"
)

// minSignalValue is the floor below which a normalized signal is noise rather
// than evidence.
const minSignalValue = 0.05

// The n-gram signal needs enough k-grams to be a share of something: below these
// counts a window is too short for the ratio to mean anything.
const (
	minNGramWindows = 8
	minRenameChains = 4
)

// computeSignals derives every available signal. Signals whose baseline input is
// missing are omitted rather than zeroed: zero means "benign", not "unknown".
func (s *Scorer) computeSignals(in Inputs, calibrated bool) []Signal {
	tv := in.Tree
	b := in.Baseline
	out := make([]Signal, 0, 8)

	windowSeconds := tv.WindowDuration.Seconds()
	if windowSeconds <= 0 {
		windowSeconds = 1
	}

	if calibrated {
		if sg, ok := entropySignal(tv, b); ok {
			out = append(out, sg)
		}
		if sg, ok := writeBurstSignal(tv, b, windowSeconds); ok {
			out = append(out, sg)
		}
		if sg, ok := renameBurstSignal(tv, b, windowSeconds); ok {
			out = append(out, sg)
		}
		if sg, ok := unknownExtensionSignal(tv, b); ok {
			out = append(out, sg)
		}
		if sg, ok := deleteRateSignal(tv, b, windowSeconds); ok {
			out = append(out, sg)
		}
		if sg, ok := dirFanoutSignal(tv, b); ok {
			out = append(out, sg)
		}
	}
	if sg, ok := magicSignal(tv); ok {
		out = append(out, sg)
	}
	if sg, ok := ngramRenameChainSignal(tv); ok {
		out = append(out, sg)
	}
	if sg, ok := bytesRewrittenSignal(tv); ok {
		out = append(out, sg)
	}
	if sg, ok := busDropSignal(in.BusDropped); ok {
		out = append(out, sg)
	}

	// Without a baseline there is nothing to deviate from, so a fixed
	// bulk-modification threshold stands in. Crude, but it is what makes
	// uncalibrated mode useful rather than merely safe.
	if !calibrated {
		if sg, ok := absoluteWriteRateSignal(tv, windowSeconds, s.cfg.Scoring.AbsoluteWriteRate); ok {
			out = append(out, sg)
		}
	}

	return dropNoise(out)
}

// dropNoise removes signals below the floor at which they stop being evidence.
// Without this, any process that wrote a few kilobytes produces a verdict.
func dropNoise(signals []Signal) []Signal {
	kept := signals[:0]
	for _, sg := range signals {
		if sg.Value >= minSignalValue {
			kept = append(kept, sg)
		}
	}
	return kept
}

func absoluteWriteRateSignal(tv fingerprint.TreeVector, windowSeconds, base float64) (Signal, bool) {
	if base <= 0 {
		base = 20
	}
	rate := float64(tv.Writes) / windowSeconds
	excess := rate / base
	value := clamp01((excess - 1) / 7)
	if value <= 0 {
		return Signal{}, false
	}
	return Signal{
		Name:   "write_rate_absolute",
		Class:  ClassPrimary,
		Value:  value,
		Detail: fmt.Sprintf("%.1f writes/s exceeds the uncalibrated threshold of %.0f/s", rate, base),
	}, true
}

func entropySignal(tv fingerprint.TreeVector, b *calibrate.Baseline) (Signal, bool) {
	var sum, worst float64
	var n int
	for _, es := range tv.Entropy {
		d, ok := b.Sigma(es.Ext)
		if !ok {
			continue
		}
		z := (es.H - d.Mean) / d.StdDev
		if z < 0 {
			z = 0
		}
		sum += z
		if z > worst {
			worst = z
		}
		n++
	}
	if n == 0 {
		return Signal{}, false
	}
	mean := sum / float64(n)
	return Signal{
		Name:  "entropy_deviation",
		Class: ClassPrimary,
		Value: clamp01(mean / 6),
		Detail: fmt.Sprintf("entropy +%.1fσ mean across %d samples (worst +%.1fσ)",
			mean, n, worst),
	}, true
}

func magicSignal(tv fingerprint.TreeVector) (Signal, bool) {
	if tv.MagicTotal == 0 || tv.MagicMismatch == 0 {
		return Signal{}, false
	}
	return Signal{
		Name:  "magic_mismatch",
		Class: ClassPrimary,
		Value: clamp01(float64(tv.MagicMismatch) / float64(tv.MagicTotal) * 2),
		Detail: fmt.Sprintf("magic bytes disagree with extension on %d of %d writes",
			tv.MagicMismatch, tv.MagicTotal),
	}, true
}

// ngramRenameChainSignal reads the fingerprint's sequence feature. An encryptor
// overwrites a file and then renames it to a new extension, so a window whose
// k-grams are write-then-rename chains is encryption-like; a compiler, archiver
// or backup writes without renaming, so its share stays at zero. The measure
// needs no baseline — it is a statement about the window's own sequence.
func ngramRenameChainSignal(tv fingerprint.TreeVector) (Signal, bool) {
	ng := tv.NGram
	if ng.Total < minNGramWindows || ng.RenameChains < minRenameChains {
		return Signal{}, false
	}
	// A quarter of the window being chains is ordinary file management; eight in
	// ten saturates.
	value := clamp01((ng.ChainShare - 0.25) / 0.55)
	if value <= 0 {
		return Signal{}, false
	}
	return Signal{
		Name:  "ngram_rename_chain",
		Class: ClassSecondary,
		Value: value,
		Detail: fmt.Sprintf("%d of %d %d-grams hold a write>rename chain; dominant %s x%d",
			ng.RenameChains, ng.Total, ng.K, ng.Sequence, ng.Count),
	}, true
}

func writeBurstSignal(tv fingerprint.TreeVector, b *calibrate.Baseline, windowSeconds float64) (Signal, bool) {
	rate := float64(tv.Writes) / windowSeconds
	base, sigma, ok := rateBaseline(b, tv.ProcName)
	if !ok || base <= 0 {
		return Signal{}, false
	}
	excess := rate / base
	value := clamp01((excess - 1) / 7) // 8x baseline saturates
	if value <= 0 {
		return Signal{}, false
	}
	return Signal{
		Name:  "write_burst",
		Class: ClassPrimary,
		Value: value,
		Detail: fmt.Sprintf("%.1f writes/s vs baseline %.1f/s (%.1fx, σ=%.1f)",
			rate, base, excess, sigma),
	}, true
}

func renameBurstSignal(tv fingerprint.TreeVector, b *calibrate.Baseline, windowSeconds float64) (Signal, bool) {
	if b.RenameRate.Mean <= 0 {
		return Signal{}, false
	}
	rate := float64(tv.Renames) / windowSeconds
	base := b.RenameRate.Mean
	excess := rate / base
	value := clamp01((excess - 1) / 7)
	if value <= 0 {
		return Signal{}, false
	}
	return Signal{
		Name:   "rename_burst",
		Class:  ClassPrimary,
		Value:  value,
		Detail: fmt.Sprintf("%.1f renames/s vs host baseline %.1f/s (%.1fx)", rate, base, excess),
	}, true
}

// unknownExtensionSignal counts writes to extensions this host has never seen,
// which is how a mass rename to a new suffix (".locked") shows up. It is
// cumulative, so it survives the decaying window.
func unknownExtensionSignal(tv fingerprint.TreeVector, b *calibrate.Baseline) (Signal, bool) {
	var unknown int64
	for ext, n := range tv.ExtActivity {
		if !b.KnowsExt(ext) {
			unknown += n
		}
	}
	if unknown == 0 {
		return Signal{}, false
	}
	return Signal{
		Name:  "unknown_extension_activity",
		Class: ClassPrimary,
		Value: clamp01(float64(unknown) / 50),
		Detail: fmt.Sprintf("%d writes to extensions never seen on this host (%s)",
			unknown, topUnknownExts(tv.ExtActivity, b, 3)),
	}, true
}

func deleteRateSignal(tv fingerprint.TreeVector, b *calibrate.Baseline, windowSeconds float64) (Signal, bool) {
	if b.DeleteRate.Mean <= 0 {
		return Signal{}, false
	}
	rate := float64(tv.Deletes) / windowSeconds
	base := b.DeleteRate.Mean
	excess := rate / base
	value := clamp01((excess - 1) / 7)
	if value <= 0 {
		return Signal{}, false
	}
	return Signal{
		Name:   "delete_rate",
		Class:  ClassSecondary,
		Value:  value,
		Detail: fmt.Sprintf("%.1f deletes/s vs host baseline %.1f/s (%.1fx)", rate, base, excess),
	}, true
}

func dirFanoutSignal(tv fingerprint.TreeVector, b *calibrate.Baseline) (Signal, bool) {
	if b.DirFanout.Mean <= 0 {
		return Signal{}, false
	}
	dirs := tv.DirCount()
	excess := float64(dirs) / b.DirFanout.Mean
	value := clamp01((excess - 1) / 7)
	if value <= 0 {
		return Signal{}, false
	}
	return Signal{
		Name:   "dir_fanout",
		Class:  ClassSecondary,
		Value:  value,
		Detail: fmt.Sprintf("touched %d directories vs baseline %.1f", dirs, b.DirFanout.Mean),
	}, true
}

func bytesRewrittenSignal(tv fingerprint.TreeVector) (Signal, bool) {
	const saturate = 1 << 30 // 1 GiB
	if tv.CumBytesRewritten <= 0 {
		return Signal{}, false
	}
	value := clamp01(float64(tv.CumBytesRewritten) / saturate)
	if value <= 0 {
		return Signal{}, false
	}
	return Signal{
		Name:  "cum_bytes_rewritten",
		Class: ClassSecondary,
		Value: value,
		Detail: fmt.Sprintf("%s rewritten across %d files since process start",
			humanBytes(tv.CumBytesRewritten), tv.CumFilesRewritten),
	}, true
}

// busDropSignal treats overload as evidence: a storm that saturates the bus also
// means the window under-counted.
func busDropSignal(dropped uint64) (Signal, bool) {
	if dropped == 0 {
		return Signal{}, false
	}
	return Signal{
		Name:   "bus_drops",
		Class:  ClassSecondary,
		Value:  clamp01(float64(dropped) / 1000),
		Detail: fmt.Sprintf("%d events dropped by the bus (window under-counts)", dropped),
	}, true
}

// rateBaseline prefers the per-process baseline and falls back to the host-wide
// write rate, because ransomware is a new process no per-process baseline
// contains. Both are write rates: comparing a write rate against an all-event
// rate made this signal unreachable on a host with many processes.
func rateBaseline(b *calibrate.Baseline, procName string) (base, sigma float64, ok bool) {
	if procName != "" {
		if v, found := b.WriteRateByProc[procName]; found && v > 0 {
			return v, b.WriteRate.StdDev, true
		}
	}
	if b.WriteRate.Mean > 0 {
		return b.WriteRate.Mean, b.WriteRate.StdDev, true
	}
	return 0, 0, false
}

func topUnknownExts(m map[string]int64, b *calibrate.Baseline, n int) string {
	type pair struct {
		ext string
		n   int64
	}
	items := make([]pair, 0, len(m))
	for ext, c := range m {
		if !b.KnowsExt(ext) {
			items = append(items, pair{ext, c})
		}
	}
	if len(items) == 0 {
		return "none"
	}
	sort.Slice(items, func(i, j int) bool { return items[i].n > items[j].n })
	if len(items) > n {
		items = items[:n]
	}
	out := ""
	for i, it := range items {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%s x%d", it.ext, it.n)
	}
	return out
}

func clamp01(x float64) float64 {
	switch {
	case x < 0:
		return 0
	case x > 1:
		return 1
	default:
		return x
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
