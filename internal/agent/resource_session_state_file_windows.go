//go:build windows

package agent

import "golang.org/x/sys/windows"

func replaceResourceSessionStateFile(source, destination string) error {
	sourcePath, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPath, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourcePath,
		destinationPath,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}

// Windows has no directory fsync equivalent. MoveFileEx with
// MOVEFILE_WRITE_THROUGH above waits for the replacement to reach storage.
func syncResourceSessionStateDirectory(string) error {
	return nil
}
