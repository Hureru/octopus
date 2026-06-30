package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/webdav"
)

func WebDAVBackupTask() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := webdav.RunBackup(ctx); err != nil {
		log.Warnf("webdav backup task failed: %v", err)
	}
}
