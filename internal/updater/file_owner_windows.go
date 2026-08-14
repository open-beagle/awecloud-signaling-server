//go:build windows

package updater

import "os"

func ownedByCurrentUser(os.FileInfo) bool {
	return true
}
