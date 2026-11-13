package deviceapi

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

// The one-time enrollment code is an opaque bearer secret of the control plane.
// Firmware only carries it from the installation command to Gateway: it checks
// the outer bounds, holds the value on tmpfs for at most an hour and never
// redeems, logs or reports it.
const EnrollmentCapability = "urn:amnezia-gpt:device-service:enrollment-code:v1"
const ErrEnrollmentCode Error = "invalid_enrollment_code"
const enrollmentFile = "enrollment-code"
const enrollmentTTL = time.Hour

var enrollmentPattern = regexp.MustCompile(`^agpt_enr_[A-Za-z0-9_-]{43}$`)

func ValidEnrollmentCode(code string) bool { return enrollmentPattern.MatchString(code) }

// CodeStore keeps at most one code as a private file in the installation's
// root-only runtime directory, which is never bound into a container.
type CodeStore struct {
	dir string
	now func() time.Time
}

func (s *CodeStore) path() string { return filepath.Join(s.dir, enrollmentFile) }

// Put replaces any held code; receipt time starts its local lifetime.
func (s *CodeStore) Put(code string) error {
	if !ValidEnrollmentCode(code) {
		return ErrEnrollmentCode
	}
	staged := s.path() + ".new"
	if err := os.Remove(staged); err != nil && !os.IsNotExist(err) {
		return ErrKeystore
	}
	f, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return ErrKeystore
	}
	_, writeErr := f.WriteString(code)
	closeErr := f.Close()
	at := s.now()
	if writeErr != nil || closeErr != nil || os.Chtimes(staged, at, at) != nil || os.Rename(staged, s.path()) != nil {
		os.Remove(staged)
		return ErrKeystore
	}
	return nil
}

// Get returns the held code. An expired, foreign or malformed file is removed.
func (s *CodeStore) Get() (string, bool) {
	f, err := os.OpenFile(s.path(), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if !os.IsNotExist(err) {
			os.Remove(s.path())
		}
		return "", false
	}
	defer f.Close()
	info, err := f.Stat()
	st, ok := info.Sys().(*syscall.Stat_t)
	age := s.now().Sub(info.ModTime())
	if err != nil || !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || age < -time.Minute || age >= enrollmentTTL {
		os.Remove(s.path())
		return "", false
	}
	b, err := io.ReadAll(io.LimitReader(f, 64))
	if err != nil || !ValidEnrollmentCode(string(b)) {
		os.Remove(s.path())
		return "", false
	}
	return string(b), true
}

func (s *CodeStore) Held() bool { _, held := s.Get(); return held }

func (s *CodeStore) Delete() error {
	if err := os.Remove(s.path()); err != nil && !os.IsNotExist(err) {
		return ErrKeystore
	}
	return nil
}

// EnrollmentCodes opens the store of one installation, creating the root-only
// runtime directories the service would otherwise create at its first start.
func EnrollmentCodes(installation string, create bool) (*CodeStore, error) {
	if os.Geteuid() != 0 || !installationPattern.MatchString(installation) {
		return nil, ErrKeystore
	}
	base := "/run/agent-firmware/" + installation
	if create && (ensureDir(filepath.Dir(base), 0700, 0, 0) != nil || ensureDir(base, 0700, 0, 0) != nil) {
		return nil, ErrKeystore
	}
	return &CodeStore{dir: base, now: time.Now}, nil
}
