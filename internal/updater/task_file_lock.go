package updater

import (
	"fmt"
	"os"
	"path/filepath"
)

func acquireTaskFileLock(stateDir, taskID, purpose string) (func() error, error) {
	tasksDir := filepath.Join(stateDir, "tasks")
	if err := os.MkdirAll(tasksDir, 0700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(tasksDir, fmt.Sprintf(".%s.%s.lock", taskID, purpose))
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	releasePlatformLock, err := lockTaskFile(file)
	if err != nil {
		file.Close()
		return nil, err
	}
	return func() error {
		unlockErr := releasePlatformLock()
		closeErr := file.Close()
		if unlockErr != nil {
			return unlockErr
		}
		return closeErr
	}, nil
}
