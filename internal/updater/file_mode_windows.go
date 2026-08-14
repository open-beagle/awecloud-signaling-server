//go:build windows

package updater

import "os"

func secureTaskFileMode(os.FileInfo) bool {
	return true
}
