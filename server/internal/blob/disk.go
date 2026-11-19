package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

var keyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{22}/[A-Za-z0-9_-]{22}$`)

var ErrKey = errors.New("invalid object key")

type Disk struct {
	root string
}

func (d *Disk) Root() string { return d.root }

func NewDisk(root string) (*Disk, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, err
	}
	return &Disk{root: abs}, nil
}

func ValidKey(key string) bool {
	return keyRe.MatchString(key)
}

func (d *Disk) resolve(key string) (string, error) {
	if !ValidKey(key) {
		return "", ErrKey
	}
	p := filepath.Join(d.root, filepath.FromSlash(key))
	if !strings.HasPrefix(p, d.root+string(os.PathSeparator)) {
		return "", ErrKey
	}
	return p, nil
}

func (d *Disk) partPath(key string, part uint16) (string, error) {
	p, err := d.resolve(key)
	if err != nil {
		return "", err
	}
	if part == 0 {
		return p, nil
	}
	return fmt.Sprintf("%s.p%d", p, part), nil
}

// Write stores one part. part 0 replaces the final object.
func (d *Disk) Write(key string, part uint16, r io.Reader, maxBytes int64) (int64, string, error) {
	path, err := d.partPath(key, part)
	if err != nil {
		return 0, "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return 0, "", err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, "", err
	}
	h := sha256.New()
	n, copyErr := io.Copy(f, io.TeeReader(io.LimitReader(r, maxBytes+1), h))
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		os.Remove(tmp)
		if copyErr != nil {
			return 0, "", copyErr
		}
		if syncErr != nil {
			return 0, "", syncErr
		}
		return 0, "", closeErr
	}
	if n > maxBytes {
		os.Remove(tmp)
		return 0, "", fmt.Errorf("object exceeds %d bytes", maxBytes)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func (d *Disk) Size(key string) (int64, error) {
	path, err := d.resolve(key)
	if err != nil {
		return 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func (d *Disk) Open(key string) (*os.File, error) {
	path, err := d.resolve(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (d *Disk) Assemble(key string, parts int) (int64, error) {
	if parts < 1 {
		return 0, errors.New("no parts")
	}
	final, err := d.resolve(key)
	if err != nil {
		return 0, err
	}
	tmp := final + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, err
	}
	var total int64
	cleanup := func() {
		out.Close()
		os.Remove(tmp)
	}
	for i := 1; i <= parts; i++ {
		pp, err := d.partPath(key, uint16(i))
		if err != nil {
			cleanup()
			return 0, err
		}
		in, err := os.Open(pp)
		if err != nil {
			cleanup()
			return 0, err
		}
		n, err := io.Copy(out, in)
		in.Close()
		if err != nil {
			cleanup()
			return 0, err
		}
		total += n
	}
	if err := out.Sync(); err != nil {
		cleanup()
		return 0, err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	for i := 1; i <= parts; i++ {
		pp, err := d.partPath(key, uint16(i))
		if err == nil {
			os.Remove(pp)
		}
	}
	return total, nil
}

func (d *Disk) Delete(key string) error {
	path, err := d.resolve(key)
	if err != nil {
		return err
	}
	os.Remove(path)
	matches, _ := filepath.Glob(path + ".p*")
	for _, m := range matches {
		os.Remove(m)
	}
	os.Remove(path + ".tmp")
	return nil
}

func FreeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
