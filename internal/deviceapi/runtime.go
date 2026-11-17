package deviceapi

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
)

// Runtime owns one installation's socket. Directories stay stable across restarts
// because Gateway binds directories rather than a replaceable socket inode.
type Runtime struct {
	Vault    *Vault
	Watch    *Watcher
	Codes    *CodeStore
	server   *http.Server
	listener net.Listener
	socket   string
	inode    os.FileInfo
	done     chan struct{}
	Events   <-chan Event
}

func Start(stateDir, installation string, key []byte) (*Runtime, error) {
	if os.Geteuid() != 0 || !installationPattern.MatchString(installation) {
		return nil, ErrKeystore
	}
	return startAt(stateDir, host.DefaultLayout().InstallationRuntime(installation), installation, key)
}
func ensureDir(path string, mode os.FileMode, uid, gid int) error {
	if err := os.Mkdir(path, mode); err == nil {
		if os.Chown(path, uid, gid) != nil || os.Chmod(path, mode) != nil {
			return ErrKeystore
		}
	} else if !os.IsExist(err) {
		return ErrKeystore
	}
	i, err := os.Lstat(path)
	owner := privatefile.Owner{UID: uint32(uid), GID: uint32(gid)}
	if err != nil || !owner.Directory(i, mode) {
		return ErrKeystore
	}
	return nil
}
func startAt(stateDir, base, installation string, key []byte) (*Runtime, error) {
	v, err := OpenVault(stateDir, installation, key)
	if err != nil {
		return nil, err
	}
	good := false
	defer func() {
		if !good {
			v.Close()
		}
	}()
	if ensureDir(filepath.Dir(base), 0700, 0, 0) != nil || ensureDir(base, 0700, 0, 0) != nil || ensureDir(base+"/api", 0750, 0, int(GatewayUID)) != nil || ensureDir(base+"/auth", 0500, int(GatewayUID), int(GatewayUID)) != nil {
		return nil, ErrKeystore
	}
	tokenPath := base + "/auth/token"
	if _, err := os.Lstat(tokenPath); os.IsNotExist(err) {
		f, err := privatefile.Open(nil, tokenPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0400)
		if err != nil {
			return nil, ErrKeystore
		}
		_, writeErr := f.WriteString(v.token())
		chownErr := f.Chown(int(GatewayUID), int(GatewayUID))
		closeErr := f.Close()
		if writeErr != nil || chownErr != nil || closeErr != nil {
			return nil, ErrKeystore
		}
	}
	i, err := os.Lstat(tokenPath)
	if err != nil || !i.Mode().IsRegular() || i.Mode().Perm() != 0400 {
		return nil, ErrKeystore
	}
	owner, links, ok := privatefile.Ownership(i)
	if !ok || owner.UID != GatewayUID || owner.GID != GatewayUID || links != 1 {
		return nil, ErrKeystore
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil || string(token) != v.token() {
		return nil, ErrKeystore
	}
	path := base + "/api/socket"
	if old, err := os.Lstat(path); err == nil {
		owner, _, ok := privatefile.Ownership(old)
		if old.Mode()&os.ModeSocket == 0 || !ok || owner.UID != 0 || owner.GID != GatewayUID || old.Mode().Perm() != 0660 {
			return nil, ErrKeystore
		}
		if os.Remove(path) != nil {
			return nil, ErrKeystore
		}
	} else if !os.IsNotExist(err) {
		return nil, ErrKeystore
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, ErrKeystore
	}
	listener.SetUnlinkOnClose(false)
	if os.Chown(path, 0, int(GatewayUID)) != nil || os.Chmod(path, 0660) != nil {
		listener.Close()
		os.Remove(path)
		return nil, ErrKeystore
	}
	inode, err := os.Lstat(path)
	if err != nil {
		listener.Close()
		return nil, ErrKeystore
	}
	watch := NewWatcher()
	handler := NewHandler(v, watch)
	watch.diagnostics = handler.diagnostics
	handler.enrollment = &CodeStore{dir: base, now: time.Now}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 500 * time.Millisecond, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 * 1024, ErrorLog: log.New(io.Discard, "", 0), ConnContext: func(ctx context.Context, c net.Conn) context.Context {
		uid, err := peerUID(c)
		if err != nil {
			return ctx
		}
		return context.WithValue(ctx, peerKey{}, uid)
	}}
	rt := &Runtime{Vault: v, Watch: watch, Codes: handler.enrollment, server: server, listener: listener, socket: path, inode: inode, done: make(chan struct{}), Events: handler.diagnostics.events}
	handler.diagnostics.record("device_api_request", "keystore", "start", "ready", 0)
	// Limit accepted connections before HTTP creates a goroutine for each one.
	bounded := &boundedListener{Listener: listener, slots: make(chan struct{}, 16), done: make(chan struct{})}
	rt.listener = bounded
	go func() { defer close(rt.done); server.Serve(bounded) }()
	good = true
	return rt, nil
}
func (r *Runtime) Close() error {
	r.Watch.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	vaultClosed := make(chan error, 1)
	go func() { vaultClosed <- r.Vault.Close() }()
	err := r.server.Shutdown(ctx)
	if err != nil {
		r.server.Close()
	}
	r.listener.Close()
	<-r.done
	if info, e := os.Lstat(r.socket); e == nil && os.SameFile(info, r.inode) {
		os.Remove(r.socket)
	}
	if <-vaultClosed != nil {
		return ErrKeystore
	}
	return err
}

// LoadCredential reads only the systemd-decrypted, root-owned credential file.
func LoadCredential(directory string) ([]byte, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, ErrKeystore
	}
	r, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrKeystore
	}
	defer r.Close()
	f, err := privatefile.Open(r, "device-api-key", os.O_RDONLY, 0)
	if err != nil {
		return nil, ErrKeystore
	}
	defer f.Close()
	i, err := f.Stat()
	if err != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 {
		return nil, ErrKeystore
	}
	owner, links, ok := privatefile.Ownership(i)
	if !ok || owner.UID != 0 || links != 1 {
		return nil, ErrKeystore
	}
	b, err := io.ReadAll(io.LimitReader(f, 33))
	if err != nil || len(b) != 32 {
		return nil, ErrKeystore
	}
	return b, nil
}

type boundedListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}
type boundedConn struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *boundedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }
func (l *boundedListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &boundedConn{Conn: c, release: func() { <-l.slots }}, nil
}
func (l *boundedListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

func (r *Runtime) Running() bool {
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}
