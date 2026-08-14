//go:build !windows

package updater

import "os"

func secureTaskFileMode(info os.FileInfo) bool {
	return info.Mode().Perm() == 0600
}
