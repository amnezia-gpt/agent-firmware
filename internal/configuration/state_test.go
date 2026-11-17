package configuration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func testInput(t *testing.T) *Input {
	t.Helper()
	p, err := os.ReadFile("../backendprofile/testdata/profile-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := os.ReadFile("../workloadrelease/testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	input, err := Parse(bytes.NewReader(p), bytes.NewReader(r))
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func testDevice(t *testing.T) *Device {
	t.Helper()
	input := testInput(t)
	device, err := ParseDevice(bytes.NewReader(input.releaseBytes))
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func testStore(t *testing.T) Store {
	t.Helper()
	return Store{Path: filepath.Join(t.TempDir(), "state")}
}

func TestDeviceReleaseSurvivesReopenAndEquivalentUpdateIsNoOp(t *testing.T) {
	input := testDevice(t)
	store := testStore(t)
	changed, err := store.Manufacture(input, nil)
	if err != nil || !changed {
		t.Fatalf("first save: changed=%v err=%v", changed, err)
	}
	loaded, err := (Store{Path: store.Path}).LoadDevice()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation() != input.Generation() || !bytes.Equal(loaded.raw, input.raw) {
		t.Fatal("input changed across reopen")
	}
	before, err := os.ReadFile(filepath.Join(store.Path, "device.json"))
	if err != nil {
		t.Fatal(err)
	}
	equivalent, err := ParseDevice(bytes.NewReader(append(append([]byte(nil), input.raw...), '\n')))
	if err != nil {
		t.Fatal(err)
	}
	changed, err = store.Manufacture(equivalent, nil)
	if err != nil || changed {
		t.Fatalf("equivalent save: changed=%v err=%v", changed, err)
	}
	after, err := os.ReadFile(filepath.Join(store.Path, "device.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("NoOp replaced retained input")
	}
	for _, name := range []string{"device.json", "operation.lock"} {
		info, err := os.Stat(filepath.Join(store.Path, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("unsafe file mode")
		}
	}
	info, err := os.Stat(store.Path)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("unsafe directory mode")
	}
}

func TestCommitFailuresPreserveOldOrReportUncertainty(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before rename", true: "after rename"}[after], func(t *testing.T) {
			store := testStore(t)
			old := testDevice(t)
			if _, err := store.Manufacture(old, nil); err != nil {
				t.Fatal(err)
			}
			raw := bytes.ReplaceAll(old.raw, []byte("v0.1.0"), []byte("v0.2.0"))
			next, err := ParseDevice(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			failure := func() error { return errors.New("sensitive filesystem failure") }
			want := ErrWrite
			if after {
				store.syncDirectory = func(*os.File) error { return failure() }
				want = ErrUncertain
			} else {
				store.beforeRename = failure
			}
			if _, err := store.Manufacture(next, nil); !errors.Is(err, want) {
				t.Fatalf("error=%v, want %v", err, want)
			}
			loaded, err := store.LoadDevice()
			if err != nil {
				t.Fatal(err)
			}
			expected := old.Generation()
			if after {
				expected = next.Generation()
			}
			if loaded.Generation() != expected {
				t.Fatal("unexpected committed generation")
			}
		})
	}
}

func TestWriterContentionIsNonblocking(t *testing.T) {
	store := testStore(t)
	input := testDevice(t)
	if _, err := store.Manufacture(input, nil); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(store.Path, "operation.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if _, err := store.Manufacture(input, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("contention: %v", err)
	}
	if _, err := store.LoadDevice(); err != nil {
		t.Fatal("reader blocked on writer lock")
	}
}

func TestLoadRejectsCorruptionAndIgnoresUncommittedFiles(t *testing.T) {
	store := testStore(t)
	input := testDevice(t)
	if _, err := store.Manufacture(input, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Path, "pending-abandoned"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadDevice(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Path, "device.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(input.Generation()), []byte("sha256:"+strings.Repeat("0", 64)), 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadDevice(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corruption: %v", err)
	}
	if _, err := store.Manufacture(input, nil); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("overwriting corrupt state: %v", err)
	}
}

func TestStateRejectsUnsafeModesAndSymlinks(t *testing.T) {
	for _, name := range []string{"directory mode", "file mode", "state symlink", "lock symlink", "directory symlink", "lock fifo"} {
		t.Run(name, func(t *testing.T) {
			store := testStore(t)
			input := testDevice(t)
			if _, err := store.Manufacture(input, nil); err != nil {
				t.Fatal(err)
			}
			accepted := filepath.Join(store.Path, "device.json")
			lock := filepath.Join(store.Path, "operation.lock")
			switch name {
			case "directory mode":
				if err := os.Chmod(store.Path, 0755); err != nil {
					t.Fatal(err)
				}
			case "file mode":
				if err := os.Chmod(accepted, 0644); err != nil {
					t.Fatal(err)
				}
			case "state symlink", "lock symlink":
				path := accepted
				if name == "lock symlink" {
					path = lock
				}
				if err := os.Rename(path, path+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".real", path); err != nil {
					t.Fatal(err)
				}
			case "directory symlink":
				if err := os.Rename(store.Path, store.Path+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(store.Path+".real", store.Path); err != nil {
					t.Fatal(err)
				}
			case "lock fifo":
				if err := os.Remove(lock); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(lock, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.Manufacture(input, nil); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("unsafe state accepted: %v", err)
			}
		})
	}
}

func TestReadAbsentStateDoesNotCreateFiles(t *testing.T) {
	store := testStore(t)
	if _, err := store.LoadDevice(); !errors.Is(err, ErrMissing) {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Lstat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created state directory")
	}
}

func TestProcessInterruptionAtCommitBoundary(t *testing.T) {
	if stage := os.Getenv("FIRMWARE_TEST_CRASH_STAGE"); stage != "" {
		input := testDevice(t)
		raw := bytes.ReplaceAll(input.raw, []byte("v0.1.0"), []byte("v0.2.0"))
		next, err := ParseDevice(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		store := Store{Path: os.Getenv("FIRMWARE_TEST_STATE_DIR")}
		if stage == "before" {
			store.beforeRename = func() error { os.Exit(23); return nil }
		} else {
			store.syncDirectory = func(*os.File) error { os.Exit(23); return nil }
		}
		store.Manufacture(next, nil)
		t.Fatal("helper did not terminate")
	}
	for _, stage := range []string{"before", "after"} {
		t.Run(stage, func(t *testing.T) {
			store := testStore(t)
			input := testDevice(t)
			if _, err := store.Manufacture(input, nil); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessInterruptionAtCommitBoundary$")
			command.Env = append(os.Environ(), "FIRMWARE_TEST_CRASH_STAGE="+stage, "FIRMWARE_TEST_STATE_DIR="+store.Path)
			err := command.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("helper exit: %v", err)
			}
			loaded, err := store.LoadDevice()
			if err != nil {
				t.Fatal(err)
			}
			if (loaded.Generation() == input.Generation()) != (stage == "before") {
				t.Fatal("wrong generation after process termination")
			}
			if _, err := store.Manufacture(loaded, nil); err != nil {
				t.Fatalf("process exit did not release lock: %v", err)
			}
		})
	}
}

func TestNoOpStillAcknowledgesDirectorySync(t *testing.T) {
	store := testStore(t)
	input := testDevice(t)
	if _, err := store.Manufacture(input, nil); err != nil {
		t.Fatal(err)
	}
	store.syncDirectory = func(*os.File) error { return errors.New("sync failed") }
	if _, err := store.Manufacture(input, nil); !errors.Is(err, ErrUncertain) {
		t.Fatalf("NoOp acknowledged an unsynchronized directory: %v", err)
	}
}

func TestConcurrentReadersSeeCompleteAcceptedSnapshots(t *testing.T) {
	store := testStore(t)
	old := testDevice(t)
	raw := bytes.ReplaceAll(old.raw, []byte("v0.1.0"), []byte("v0.2.0"))
	next, err := ParseDevice(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(old, nil); err != nil {
		t.Fatal(err)
	}
	var readers sync.WaitGroup
	start := make(chan struct{})
	failures := make(chan error, 1)
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for j := 0; j < 300; j++ {
				loaded, err := store.LoadDevice()
				if err == nil && loaded.Generation() != old.Generation() && loaded.Generation() != next.Generation() {
					err = errors.New("mixed snapshot")
				}
				if err != nil {
					select {
					case failures <- err:
					default:
					}
					return
				}
			}
		}()
	}
	close(start)
	for i := 0; i < 30; i++ {
		value := old
		if i%2 == 0 {
			value = next
		}
		if _, err := store.Manufacture(value, nil); err != nil {
			t.Fatal(err)
		}
	}
	readers.Wait()
	select {
	case err := <-failures:
		t.Fatalf("reader during atomic replacement: %v", err)
	default:
	}
}

func TestDirectorySymlinkWithTrailingSlashIsRejected(t *testing.T) {
	store := testStore(t)
	input := testDevice(t)
	if _, err := store.Manufacture(input, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(store.Path, store.Path+".real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store.Path+".real", store.Path); err != nil {
		t.Fatal(err)
	}
	store.Path += "/"
	if _, err := store.LoadDevice(); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("trailing slash bypassed directory symlink guard: %v", err)
	}
}

func TestExclusiveSessionSharesWriterLockAndPersistsOperation(t *testing.T) {
	store := testStore(t)
	input := testDevice(t)
	if _, err := store.Manufacture(input, nil); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"operation":"example"}`)
	if err := store.Exclusive(func(session *Session) error {
		if _, err := store.Manufacture(input, nil); !errors.Is(err, ErrBusy) {
			t.Fatalf("configure did not share apply lock: %v", err)
		}
		loaded, err := loadDevice(session.root)
		if err != nil || loaded.Generation() != input.Generation() {
			t.Fatal("session did not load accepted input")
		}
		return session.Write(want)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(input, nil); !errors.Is(err, ErrPending) {
		t.Fatalf("configure overwrote pending operation: %v", err)
	}
	if err := store.Exclusive(func(session *Session) error {
		got, err := session.Read()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("journal did not survive reopening")
		}
		return session.Write(nil)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Manufacture(input, nil); err != nil {
		t.Fatal(err)
	}
}
