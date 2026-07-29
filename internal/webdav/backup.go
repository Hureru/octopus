package webdav

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/studio-b12/gowebdav"
)

const (
	backupPrefix            = "octopus-backup-"
	backupJSONSuffix        = ".json"
	backupZipSuffix         = ".zip"
	maxWebDAVBackupFileSize = 128 << 20
)

type BackupInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

func RunBackup(ctx context.Context) error {
	return runBackupWithLimit(ctx, maxWebDAVBackupFileSize)
}

func runBackupWithLimit(ctx context.Context, maxSize int64) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp("", "octopus-webdav-backup-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temporary backup: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	if err := op.DBExportZip(ctx, temp, false, cfg.IncludeStats); err != nil {
		return fmt.Errorf("failed to export database: %w", err)
	}
	stat, err := temp.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat temporary backup: %w", err)
	}
	if stat.Size() > maxSize {
		return fmt.Errorf("backup exceeds upload size limit")
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to rewind temporary backup: %w", err)
	}

	c, err := NewClient(cfg)
	if err != nil {
		return err
	}
	_ = c.MkdirAll(cfg.BackupPath, 0755)

	filename := backupFilename()
	remotePath := path.Join(cfg.BackupPath, filename)

	if err := c.WriteStreamWithLength(remotePath, temp, stat.Size(), 0644); err != nil {
		return fmt.Errorf("failed to upload backup: %w", err)
	}

	log.Infof("webdav backup uploaded: %s (%d bytes)", filename, stat.Size())

	enforceRetention(c, cfg.BackupPath, cfg.RetentionCount)
	return nil
}

func backupFilename() string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return backupPrefix + time.Now().UTC().Format("20060102150405.000000000") + backupZipSuffix
	}
	return backupPrefix +
		time.Now().UTC().Format("20060102150405.000000000") +
		"-" + hex.EncodeToString(suffix[:]) +
		backupZipSuffix
}

func ListBackups(ctx context.Context) ([]BackupInfo, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	c, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	files, err := c.ReadDir(cfg.BackupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list backup directory: %w", err)
	}

	var backups []BackupInfo
	for _, f := range files {
		if f.IsDir() || !isBackupFile(f.Name()) {
			continue
		}
		backups = append(backups, BackupInfo{
			Name:       f.Name(),
			Size:       f.Size(),
			ModifiedAt: f.ModTime(),
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Name > backups[j].Name
	})
	return backups, nil
}

func RestoreFromBackup(ctx context.Context, filename string) (*model.DBImportResult, error) {
	if !isBackupFile(filename) {
		return nil, fmt.Errorf("invalid backup filename")
	}

	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	c, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	remotePath := path.Join(cfg.BackupPath, filename)

	stream, err := c.ReadStream(remotePath)
	if err != nil {
		return nil, fmt.Errorf("failed to download backup: %w", err)
	}
	defer stream.Close()

	temp, err := os.CreateTemp("", "octopus-webdav-restore-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary restore file: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()
	written, err := io.Copy(temp, io.LimitReader(stream, maxWebDAVBackupFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to download backup: %w", err)
	}
	if written > maxWebDAVBackupFileSize {
		return nil, fmt.Errorf("backup exceeds restore size limit")
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to rewind restore file: %w", err)
	}

	var result *model.DBImportResult
	if strings.HasSuffix(filename, backupZipSuffix) {
		result, err = op.DBImportZip(ctx, temp, written)
	} else {
		var dump model.DBDump
		decoder := json.NewDecoder(temp)
		if err := decoder.Decode(&dump); err != nil {
			return nil, fmt.Errorf("failed to parse backup: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				return nil, fmt.Errorf("failed to parse backup: trailing JSON value")
			}
			return nil, fmt.Errorf("failed to parse backup: %w", err)
		}
		result, err = op.DBImportIncremental(ctx, &dump)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to import backup: %w", err)
	}

	if err := op.InitCache(); err != nil {
		log.Warnf("cache refresh after webdav restore failed: %v", err)
	}

	return result, nil
}

func enforceRetention(c *gowebdav.Client, backupPath string, count int) {
	files, err := c.ReadDir(backupPath)
	if err != nil {
		return
	}

	var backupNames []string
	for _, f := range files {
		if !f.IsDir() && isBackupFile(f.Name()) {
			backupNames = append(backupNames, f.Name())
		}
	}

	sort.Strings(backupNames)

	if len(backupNames) <= count {
		return
	}

	toDelete := backupNames[:len(backupNames)-count]
	for _, name := range toDelete {
		remotePath := path.Join(backupPath, name)
		if err := c.Remove(remotePath); err != nil {
			log.Warnf("failed to delete old backup %s: %v", name, err)
		}
	}
}

func isBackupFile(name string) bool {
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return false
	}
	return strings.HasPrefix(name, backupPrefix) &&
		(strings.HasSuffix(name, backupJSONSuffix) || strings.HasSuffix(name, backupZipSuffix))
}
