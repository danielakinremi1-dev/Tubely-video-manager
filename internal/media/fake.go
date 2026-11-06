package media

import (
	"context"
	"io"
	"os"
)

// Fake is a Processor for tests. Probe returns Info (or ProbeErr) without
// running ffprobe, and FastStart copies the file (or returns FastStartErr).
// OnProbe and OnFastStart run inside those calls, so a test can make
// something happen mid-upload, such as the video being deleted.
type Fake struct {
	Info         Info
	ProbeErr     error
	FastStartErr error
	OnProbe      func()
	OnFastStart  func()
}

var _ Processor = (*Fake)(nil)

func (f *Fake) Probe(ctx context.Context, path string) (Info, error) {
	if f.OnProbe != nil {
		f.OnProbe()
	}
	if f.ProbeErr != nil {
		return Info{}, f.ProbeErr
	}
	return f.Info, nil
}

func (f *Fake) FastStart(ctx context.Context, path string) (string, error) {
	if f.OnFastStart != nil {
		f.OnFastStart()
	}
	if f.FastStartErr != nil {
		return "", f.FastStartErr
	}
	newPath := path + ".processing"
	if err := copyFile(path, newPath); err != nil {
		os.Remove(newPath)
		return "", err
	}
	return newPath, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
