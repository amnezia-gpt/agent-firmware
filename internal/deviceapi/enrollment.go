package deviceapi

import (
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

// The one-time enrollment code is an opaque bearer secret of the control plane.
// Firmware only carries it from enroll to the workload: it checks
// the outer bounds, holds the value on tmpfs for at most an hour and never
// redeems, logs or reports it.
const EnrollmentCapability = "urn:amnezia-gpt:device-service:enrollment-code:v1"
const ErrEnrollmentCode Error = "invalid_enrollment_code"
const enrollmentFile = "enrollment-code"
const enrollmentTTL = time.Hour

// The code is opaque here: only its alphabet and a sane length are checked. Its
// prefix and exact length belong to the authorization server and may change
// without a firmware release.
const MaxEnrollmentCode = 128

var enrollmentPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func ValidEnrollmentCode(code string) bool { return enrollmentPattern.MatchString(code) }

// CodeStore keeps at most one code as a private file in the installation's
// root-only runtime directory, which is never bound into a container.
type CodeStore struct {
	dir         string
	now         func() time.Time
	observedMu  sync.Mutex
	observed    [sha256.Size]byte
	hasObserved bool
}

func (s *CodeStore) path() string { return filepath.Join(s.dir, enrollmentFile) }

// Lock the private directory inode so separate enroll and serve processes
// serialize file replacement, consumption and removal without storing a lock
// file next to the bearer code.
func (s *CodeStore) lock() (func(), error) {
	root, err := privatefile.OpenDirectory(s.dir, privatefile.EffectiveOwner(), 0700)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, ErrKeystore
	}
	dir, err := root.Open(".")
	root.Close()
	if err != nil {
		return nil, ErrKeystore
	}
	for range 20 {
		err = privatefile.TryLock(dir)
		if err == nil {
			return func() { dir.Close() }, nil
		}
		if !errors.Is(err, privatefile.ErrBusy) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	dir.Close()
	return nil, ErrKeystore
}

// Put replaces any held code; receipt time starts its local lifetime.
func (s *CodeStore) Put(code string) error {
	if !ValidEnrollmentCode(code) {
		return ErrEnrollmentCode
	}
	unlock, err := s.lock()
	if err != nil {
		return ErrKeystore
	}
	defer unlock()
	staged := s.path() + ".new"
	if err := os.Remove(staged); err != nil && !os.IsNotExist(err) {
		return ErrKeystore
	}
	f, err := privatefile.Open(nil, staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
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
	code, _ := s.read()
	return code, code != ""
}

func (s *CodeStore) read() (string, error) {
	unlock, err := s.lock()
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer unlock()
	return s.readLocked(), nil
}

func (s *CodeStore) readLocked() string {
	f, err := privatefile.Open(nil, s.path(), os.O_RDONLY, 0)
	if err != nil {
		if !os.IsNotExist(err) {
			os.Remove(s.path())
		}
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		os.Remove(s.path())
		return ""
	}
	owner, links, ok := privatefile.Ownership(info)
	age := s.now().Sub(info.ModTime())
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || owner.UID != uint32(os.Geteuid()) || links != 1 || age < -time.Minute || age >= enrollmentTTL {
		os.Remove(s.path())
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxEnrollmentCode+1))
	if err != nil || !ValidEnrollmentCode(string(b)) {
		os.Remove(s.path())
		return ""
	}
	return string(b)
}

func (s *CodeStore) HasCode() (bool, error) {
	code, err := s.read()
	return code != "", err
}

func (s *CodeStore) Held() bool { held, _ := s.HasCode(); return held }

func (s *CodeStore) getForWorkload() (string, bool) {
	s.observedMu.Lock()
	defer s.observedMu.Unlock()
	code, held := s.Get()
	if held {
		s.observed = sha256.Sum256([]byte(code))
		s.hasObserved = true
	}
	return code, held
}

// A delayed workload acknowledgment for a replaced code must leave the new
// code in place. Host-side cleanup uses Delete and owns the attempt lock.
func (s *CodeStore) deleteForWorkload() error {
	s.observedMu.Lock()
	defer s.observedMu.Unlock()
	if !s.hasObserved {
		return s.Delete()
	}
	unlock, err := s.lock()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	code := s.readLocked()
	if code == "" || s.observed != sha256.Sum256([]byte(code)) {
		return nil
	}
	return s.remove()
}

func (s *CodeStore) Delete() error {
	unlock, err := s.lock()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unlock()
	return s.remove()
}

func (s *CodeStore) remove() error {
	if err := os.Remove(s.path()); err != nil && !os.IsNotExist(err) {
		return ErrKeystore
	}
	return nil
}

// EnrollmentCodes opens the store of one installation, creating the root-only
// runtime directories the service would otherwise create at its first start.
func EnrollmentCodes(layout host.Layout, installation string, create bool) (*CodeStore, error) {
	if os.Geteuid() != 0 || !installationPattern.MatchString(installation) {
		return nil, ErrKeystore
	}
	base := layout.InstallationRuntime(installation)
	if create && (ensureDir(filepath.Dir(base), 0700, 0, 0) != nil || ensureDir(base, 0700, 0, 0) != nil) {
		return nil, ErrKeystore
	}
	return &CodeStore{dir: base, now: time.Now}, nil
}
