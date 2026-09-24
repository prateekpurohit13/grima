//go:build !linux && !windows

package persistwatch

import "fmt"

// scanPersistence is unsupported here, so the sensor reports that it cannot
// observe rather than starting and producing nothing.
func scanPersistence() ([]entry, error) {
	return nil, fmt.Errorf("persistence watching is not implemented on this platform")
}
