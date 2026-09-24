//go:build !windows

package attrib

// newSource reports that no causal source exists on this platform. Windows
// file-system auditing is the only mechanism GRIMA implements, so every causal
// mode falls back to correlation here.
func newSource(mode string, _ sourceOptions) (Source, error) {
	return nil, unknownModeError(mode)
}
