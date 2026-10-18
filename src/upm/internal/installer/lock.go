package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/wonskur/upm/internal/utils"
)

func acquirePackageLock(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(utils.LibDir(), 0755); err != nil {
		return nil, fmt.Errorf("failed creating package state directory: %w", err)
	}
	lockFile, err := os.OpenFile(filepath.Join(utils.LibDir(), "upm.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed opening package operation lock: %w", err)
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
				_ = lockFile.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = lockFile.Close()
			return nil, fmt.Errorf("failed acquiring package operation lock: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = lockFile.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
