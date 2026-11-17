package deviceapi

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestUnauthorizedSlowBodyCannotHoldConnectionIndefinitely(t *testing.T) {
	v, _ := testVault(t)
	dir, err := os.MkdirTemp("/tmp", "af-api-http-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := dir + "/socket"
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: NewHandler(v), ReadHeaderTimeout: 500 * time.Millisecond, ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
		return context.WithValue(ctx, peerKey{}, GatewayUID)
	}}
	go server.Serve(listener)
	defer server.Close()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprint(conn, "POST /v1/crypto/seal HTTP/1.1\r\nHost: device\r\nAuthorization: Bearer invalid\r\nContent-Length: 100\r\nContent-Type: application/json\r\n\r\nx")
	began := time.Now()
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal("unauthorized body escaped deadline")
	}
	defer response.Body.Close()
	if response.StatusCode != 401 || time.Since(began) > 2500*time.Millisecond {
		t.Fatal("unauthorized body stalled")
	}
}

func TestListenerAdmitsOnlySixteenConnections(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "af-api-limit-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	raw, err := net.Listen("unix", dir+"/socket")
	if err != nil {
		t.Fatal(err)
	}
	listener := &boundedListener{Listener: raw, slots: make(chan struct{}, 16), done: make(chan struct{})}
	accepted := make(chan net.Conn)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			accepted <- c
		}
	}()
	var clients, servers []net.Conn
	defer func() {
		for _, c := range clients {
			c.Close()
		}
		for _, s := range servers {
			s.Close()
		}
		listener.Close()
		<-finished
	}()
	for i := 0; i < 17; i++ {
		c, e := net.Dial("unix", dir+"/socket")
		if e != nil {
			t.Fatal(e)
		}
		clients = append(clients, c)
		if i < 16 {
			servers = append(servers, <-accepted)
		}
	}
	select {
	case c := <-accepted:
		servers = append(servers, c)
		t.Fatal("connection limit exceeded")
	case <-time.After(20 * time.Millisecond):
	}
	servers[0].Close()
	select {
	case c := <-accepted:
		servers = append(servers, c)
	case <-time.After(time.Second):
		t.Fatal("capacity not released")
	}
}
