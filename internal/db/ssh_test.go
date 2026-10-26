package db

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// echoServer stands in for a database listening only on the SSH server.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	return ln.Addr().String()
}

type testSSHServer struct {
	addr        string
	fingerprint string
	clientKey   []byte // OpenSSH PEM of an authorized client key
}

// startSSHServer runs a minimal SSH server that accepts password "secret" or
// the generated client key, and supports direct-tcpip port forwarding.
func startSSHServer(t *testing.T) testSSHServer {
	t.Helper()
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	clientPub, clientPriv, _ := ed25519.GenerateKey(rand.Reader)
	sshClientPub, _ := ssh.NewPublicKey(clientPub)
	block, err := ssh.MarshalPrivateKey(clientPriv, "")
	if err != nil {
		t.Fatal(err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if c.User() == "deploy" && string(pw) == "secret" {
				return nil, nil
			}
			return nil, errors.New("bad password")
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() == "deploy" && string(k.Marshal()) == string(sshClientPub.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("bad key")
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSH(nc, cfg)
		}
	}()
	return testSSHServer{
		addr:        ln.Addr().String(),
		fingerprint: ssh.FingerprintSHA256(hostSigner.PublicKey()),
		clientKey:   pem.EncodeToMemory(block),
	}
}

func serveSSH(nc net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		nc.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "direct-tcpip" {
			nch.Reject(ssh.UnknownChannelType, "only direct-tcpip")
			continue
		}
		// RFC 4254 7.2: host string, port uint32, origin host, origin port.
		d := nch.ExtraData()
		hl := binary.BigEndian.Uint32(d)
		host := string(d[4 : 4+hl])
		port := binary.BigEndian.Uint32(d[4+hl:])
		target, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
		if err != nil {
			nch.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		ch, creqs, err := nch.Accept()
		if err != nil {
			target.Close()
			continue
		}
		go ssh.DiscardRequests(creqs)
		go func() {
			defer ch.Close()
			defer target.Close()
			done := make(chan struct{}, 2)
			go func() { io.Copy(target, ch); done <- struct{}{} }()
			go func() { io.Copy(ch, target); done <- struct{}{} }()
			<-done
		}()
	}
}

func tunnelFor(t *testing.T, srv testSSHServer) SSHTunnel {
	host, port, _ := net.SplitHostPort(srv.addr)
	p, _ := strconv.Atoi(port)
	return SSHTunnel{
		Host: host, Port: p, User: "deploy", HostKey: srv.fingerprint,
		KnownHostsFile: filepath.Join(t.TempDir(), "known_hosts"),
	}
}

func roundTrip(t *testing.T, addr string) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer c.Close()
	fmt.Fprintln(c, "ping")
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil || line != "ping\n" {
		t.Fatalf("echo through tunnel: %q %v", line, err)
	}
}

func TestTunnelPasswordAuth(t *testing.T) {
	srv := startSSHServer(t)
	db := echoServer(t)
	tun := tunnelFor(t, srv)
	tun.Password = "secret"
	t.Setenv("SSH_AUTH_SOCK", "")

	local, closeFn, err := openTunnel(context.Background(), tun, db)
	if err != nil {
		t.Fatalf("openTunnel: %v", err)
	}
	defer closeFn()
	roundTrip(t, local.String())
	roundTrip(t, local.String()) // a second connection over the same tunnel
}

func TestTunnelKeyAuth(t *testing.T) {
	srv := startSSHServer(t)
	db := echoServer(t)
	tun := tunnelFor(t, srv)
	// Trimmed like `dbk secret set --stdin` stores it.
	tun.PrivateKey = []byte(strings.TrimRight(string(srv.clientKey), "\n"))

	local, closeFn, err := openTunnel(context.Background(), tun, db)
	if err != nil {
		t.Fatalf("openTunnel: %v", err)
	}
	defer closeFn()
	roundTrip(t, local.String())
}

func TestTunnelRejectsWrongHostKey(t *testing.T) {
	srv := startSSHServer(t)
	tun := tunnelFor(t, srv)
	tun.Password = "secret"
	tun.HostKey = "SHA256:not-the-key"
	_, _, err := openTunnel(context.Background(), tun, echoServer(t))
	if err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatalf("expected host key mismatch, got %v", err)
	}
}

func TestTunnelUnknownHostKey(t *testing.T) {
	srv := startSSHServer(t)
	tun := tunnelFor(t, srv)
	tun.Password = "secret"
	tun.HostKey = ""
	_, _, err := openTunnel(context.Background(), tun, echoServer(t))
	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) || unknown.Fingerprint != srv.fingerprint {
		t.Fatalf("expected UnknownHostKeyError with %s, got %v", srv.fingerprint, err)
	}
}

func TestTunnelKnownHostsFile(t *testing.T) {
	srv := startSSHServer(t)
	tun := tunnelFor(t, srv)
	tun.Password = "secret"
	tun.HostKey = ""

	// Learn the key, then write it to known_hosts like ssh would.
	var key ssh.PublicKey
	cfg := &ssh.ClientConfig{User: "deploy", Auth: []ssh.AuthMethod{ssh.Password("secret")},
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error { key = k; return nil }}
	c, err := ssh.Dial("tcp", srv.addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	line := fmt.Sprintf("[%s]:%d %s", tun.Host, tun.Port, ssh.MarshalAuthorizedKey(key))
	if err := os.WriteFile(tun.KnownHostsFile, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	local, closeFn, err := openTunnel(context.Background(), tun, echoServer(t))
	if err != nil {
		t.Fatalf("openTunnel with known_hosts: %v", err)
	}
	defer closeFn()
	roundTrip(t, local.String())
}

func TestFetchSSHHostKey(t *testing.T) {
	srv := startSSHServer(t)
	tun := tunnelFor(t, srv)
	fp, err := FetchSSHHostKey(context.Background(), tun)
	if err != nil || fp != srv.fingerprint {
		t.Fatalf("FetchSSHHostKey = %q, %v; want %q", fp, err, srv.fingerprint)
	}
}

type connRecorder struct {
	fakeEngine
	got *Conn
}

func (r connRecorder) Test(_ context.Context, c Conn) error {
	*r.got = c
	// Talk to the "database" through the rewritten host/port.
	conn, err := net.Dial("tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	fmt.Fprintln(conn, "ping")
	_, err = bufio.NewReader(conn).ReadString('\n')
	return err
}

func TestTunneledEngineRewritesConn(t *testing.T) {
	srv := startSSHServer(t)
	dbAddr := echoServer(t)
	dbHost, dbPort, _ := net.SplitHostPort(dbAddr)
	port, _ := strconv.Atoi(dbPort)

	tun := tunnelFor(t, srv)
	tun.Password = "secret"
	var got Conn
	e := tunneled{connRecorder{fakeEngine: fakeEngine{name: "rec"}, got: &got}}
	err := e.Test(context.Background(), Conn{Host: dbHost, Port: port, User: "app", SSH: &tun})
	if err != nil {
		t.Fatalf("Test via tunnel: %v", err)
	}
	if got.SSH != nil || got.Host != "127.0.0.1" || got.Port == port || got.User != "app" {
		t.Fatalf("conn not rewritten to local forward: %+v", got)
	}
}

func TestTunneledEngineWithoutSSHPassesThrough(t *testing.T) {
	var got Conn
	e := tunneled{connRecorder{fakeEngine: fakeEngine{name: "rec"}, got: &got}}
	addr := echoServer(t)
	host, p, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(p)
	if err := e.Test(context.Background(), Conn{Host: host, Port: port}); err != nil {
		t.Fatal(err)
	}
	if got.Host != host || got.Port != port {
		t.Fatalf("direct conn changed: %+v", got)
	}
}

func TestPasswordSkipsAgent(t *testing.T) {
	// A live agent socket must not be used when a password is set.
	sock := filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix socket: %v", err)
	}
	defer ln.Close()
	t.Setenv("SSH_AUTH_SOCK", sock)

	methods, cleanup, err := SSHTunnel{Password: "secret"}.authMethods()
	defer cleanup()
	if err != nil || len(methods) != 2 { // password + keyboard-interactive
		t.Fatalf("expected only password methods, got %d (%v)", len(methods), err)
	}
	methods, cleanup2, _ := SSHTunnel{}.authMethods()
	defer cleanup2()
	if len(methods) != 1 {
		t.Fatalf("expected agent when nothing else is set, got %d", len(methods))
	}
}
