package op

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

type bytesBuffer = bytes.Buffer

func zipReaderFromBytes(b []byte) (*zip.Reader, error) {
	return zip.NewReader(bytes.NewReader(b), int64(len(b)))
}

func readZipFile(t *testing.T, zr *zip.Reader, name string) string {
	t.Helper()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("zip open %s: %v", name, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("zip read %s: %v", name, err)
		}
		return string(data)
	}
	return ""
}

func linesCount(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
