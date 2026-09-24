//go:build windows

package attrib

import (
	"testing"

	"golang.org/x/sys/windows"
)

// These functions are resolved by name at call time, so a mistyped symbol does
// not fail a build: it panics on an elevated host, in the one run that is
// expensive to repeat. Resolving them here keeps that failure in the test suite.
//
// The related contract — AuditQuerySystemPolicy returns a BOOLEAN, not an error
// code — cannot be exercised unelevated. Reading that return value as an errno
// turned every successful call into "Incorrect function" (error 1), which is
// what broke the first elevated verification run.
func TestAuditFunctionsResolve(t *testing.T) {
	cases := map[string]*windows.LazyProc{
		"AuditQuerySystemPolicy": procAuditQueryPolicy,
		"AuditSetSystemPolicy":   procAuditSetSystemPolicy,
		"AuditFree":              procAuditFree,
	}
	for name, proc := range cases {
		if err := proc.Find(); err != nil {
			t.Fatalf("%s does not resolve: %v", name, err)
		}
	}
}
