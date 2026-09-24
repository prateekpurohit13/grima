//go:build windows

package attrib

// newSource builds the causal source for one mode. A mode the platform cannot
// run returns an error, which the caller reports before falling back.
func newSource(mode string, opts sourceOptions) (Source, error) {
	switch mode {
	case modeAudit:
		return newAuditSource(opts.paths, opts.setup, opts.log), nil
	}
	return nil, unknownModeError(mode)
}
