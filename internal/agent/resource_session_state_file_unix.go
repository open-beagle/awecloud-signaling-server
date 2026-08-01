//go:build !windows

package agent

import "os"

func replaceResourceSessionStateFile(source, destination string) error {
	return os.Rename(source, destination)
}

func syncResourceSessionStateDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
