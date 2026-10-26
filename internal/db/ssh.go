package db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHTunnel describes an SSH server that the database is reached through.
// Secrets are already resolved from the vault or refs.
type SSHTunnel struct {
	Host string
	Port int // default 22
	User string
	// Password is used for password auth and as the passphrase of an
	// encrypted PrivateKey.
	Password   string
	PrivateKey []byte // PEM/OpenSSH private key; optional
	// HostKey pins the server's key by its SHA256 fingerprint ("SHA256:...").
	// Empty means the server must be listed in KnownHostsFile.
	HostKey        string
	KnownHostsFile string // default ~/.ssh/known_hosts
}

func (t SSHTunnel) addr() string {
	port := t.Port
	if port <= 0 {
		port = 22
	}
	return net.JoinHostPort(t.Host, strconv.Itoa(port))
}

// UnknownHostKeyError means the SSH server's key is neither pinned nor in
// known_hosts. Fingerprint is the key the server presented.
type UnknownHostKeyError struct {
	Host        string
	Fingerprint string
}

func (e *UnknownHostKeyError) Error() string {
	return fmt.Sprintf("ssh: unknown host key for %s (%s); pin it with ssh.host_key: %q or add the host to known_hosts",
		e.Host, e.Fingerprint, e.Fingerprint)
}

func (t SSHTunnel) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if t.HostKey != "" {
		return func(host string, _ net.Addr, key ssh.PublicKey) error {
			if got := ssh.FingerprintSHA256(key); got != t.HostKey {
				return fmt.Errorf("ssh: host key mismatch for %s: server sent %s, expected %s", host, got, t.HostKey)
			}
			return nil
		}, nil
	}
	path := t.KnownHostsFile
	if path == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, ".ssh", "known_hosts")
		}
	}
	var known ssh.HostKeyCallback
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			cb, err := knownhosts.New(path)
			if err != nil {
				return nil, fmt.Errorf("ssh: read %s: %w", path, err)
			}
			known = cb
		}
	}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		if known != nil {
			err := known(host, remote, key)
			var ke *knownhosts.KeyError
			if err == nil || !errors.As(err, &ke) || len(ke.Want) > 0 {
				// Accepted, or a real mismatch with a listed key.
				return err
			}
		}
		return &UnknownHostKeyError{Host: host, Fingerprint: ssh.FingerprintSHA256(key)}
	}, nil
}

func (t SSHTunnel) authMethods() ([]ssh.AuthMethod, func(), error) {
	var methods []ssh.AuthMethod
	cleanup := func() {}
	if len(t.PrivateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(t.PrivateKey)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			if t.Password == "" {
				return nil, cleanup, fmt.Errorf("ssh: private key is encrypted; set the SSH password/passphrase")
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(t.PrivateKey, []byte(t.Password))
		}
		if err != nil {
			return nil, cleanup, fmt.Errorf("ssh: parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	// Use ssh-agent only when nothing else is configured: its keys count
	// against the server's MaxAuthTries and could lock out the password.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" && len(t.PrivateKey) == 0 && t.Password == "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
			cleanup = func() { conn.Close() }
		}
	}
	if t.Password != "" {
		pw := t.Password
		methods = append(methods,
			ssh.Password(pw),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pw
				}
				return answers, nil
			}),
		)
	}
	if len(methods) == 0 {
		return nil, cleanup, fmt.Errorf("ssh: no auth method: set a private key, a password, or run ssh-agent")
	}
	return methods, cleanup, nil
}

func dialSSH(ctx context.Context, t SSHTunnel, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	if t.Host == "" {
		return nil, fmt.Errorf("ssh: host is required")
	}
	addr := t.addr()
	d := net.Dialer{Timeout: cfg.Timeout}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh: connect %s: %w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = nc.SetDeadline(dl)
	} else {
		_ = nc.SetDeadline(time.Now().Add(cfg.Timeout))
	}
	c, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		nc.Close()
		var unknown *UnknownHostKeyError
		if errors.As(err, &unknown) {
			return nil, unknown
		}
		return nil, fmt.Errorf("ssh: %s: %w", addr, err)
	}
	_ = nc.SetDeadline(time.Time{}) // handshake done; dumps can run for hours
	return ssh.NewClient(c, chans, reqs), nil
}

// FetchSSHHostKey connects to the SSH server only far enough to read its host
// key and returns the key's SHA256 fingerprint, so it can be pinned on first
// setup (trust on first use). It fails if known_hosts lists a different key.
func FetchSSHHostKey(ctx context.Context, t SSHTunnel) (string, error) {
	t.HostKey = ""
	known, err := t.hostKeyCallback()
	if err != nil {
		return "", err
	}
	var fp string
	var verifyErr error
	errGotKey := errors.New("got host key")
	cfg := &ssh.ClientConfig{
		User: t.User,
		Auth: []ssh.AuthMethod{ssh.Password("")},
		HostKeyCallback: func(host string, remote net.Addr, key ssh.PublicKey) error {
			fp = ssh.FingerprintSHA256(key)
			var unknown *UnknownHostKeyError
			if err := known(host, remote, key); err != nil && !errors.As(err, &unknown) {
				verifyErr = err
			}
			return errGotKey
		},
		Timeout: 15 * time.Second,
	}
	client, err := dialSSH(ctx, t, cfg)
	if client != nil {
		client.Close()
	}
	if verifyErr != nil {
		return "", verifyErr
	}
	if fp != "" {
		return fp, nil
	}
	if err == nil {
		err = fmt.Errorf("ssh: server sent no host key")
	}
	return "", err
}

// openTunnel connects to the SSH server and listens on a random local port,
// forwarding every connection to target (as seen from the SSH server).
func openTunnel(ctx context.Context, t SSHTunnel, target string) (local *net.TCPAddr, closeFn func(), err error) {
	hk, err := t.hostKeyCallback()
	if err != nil {
		return nil, nil, err
	}
	auth, authCleanup, err := t.authMethods()
	if err != nil {
		return nil, nil, err
	}
	client, err := dialSSH(ctx, t, &ssh.ClientConfig{
		User:            t.User,
		Auth:            auth,
		HostKeyCallback: hk,
		Timeout:         20 * time.Second,
	})
	authCleanup()
	if err != nil {
		return nil, nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		client.Close()
		return nil, nil, err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			lc, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer lc.Close()
				rc, err := client.Dial("tcp", target)
				if err != nil {
					return
				}
				defer rc.Close()
				done := make(chan struct{}, 2)
				go func() { io.Copy(rc, lc); done <- struct{}{} }()
				go func() { io.Copy(lc, rc); done <- struct{}{} }()
				<-done
			}()
		}
	}()

	var once sync.Once
	closeFn = func() {
		once.Do(func() {
			ln.Close()
			client.Close() // also unblocks in-flight copies
			wg.Wait()
		})
	}
	return ln.Addr().(*net.TCPAddr), closeFn, nil
}

// withTunnel runs fn with c rewritten to point at a local SSH port forward
// when c.SSH is set; otherwise it runs fn with c unchanged.
func withTunnel(ctx context.Context, e Engine, c Conn, fn func(Conn) error) error {
	if c.SSH == nil || c.Path != "" || e.DefaultPort() == 0 {
		return fn(c)
	}
	host := c.Host
	if host == "" || host == "localhost" {
		host = "127.0.0.1" // relative to the SSH server
	}
	port := c.Port
	if port <= 0 {
		port = e.DefaultPort()
	}
	local, closeTunnel, err := openTunnel(ctx, *c.SSH, net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	defer closeTunnel()
	c.Host = local.IP.String()
	c.Port = local.Port
	c.SSH = nil
	return fn(c)
}

// tunneled wraps an engine so every call goes through Conn.SSH when set.
type tunneled struct{ Engine }

func (t tunneled) Test(ctx context.Context, c Conn) error {
	return withTunnel(ctx, t.Engine, c, func(c Conn) error { return t.Engine.Test(ctx, c) })
}

func (t tunneled) Dump(ctx context.Context, c Conn, w io.Writer) error {
	return withTunnel(ctx, t.Engine, c, func(c Conn) error { return t.Engine.Dump(ctx, c, w) })
}

func (t tunneled) Restore(ctx context.Context, c Conn, r io.Reader) error {
	return withTunnel(ctx, t.Engine, c, func(c Conn) error { return t.Engine.Restore(ctx, c, r) })
}

func (t tunneled) ListDatabases(ctx context.Context, c Conn) ([]string, error) {
	lister, ok := t.Engine.(DatabaseLister)
	if !ok {
		return nil, nil
	}
	var dbs []string
	err := withTunnel(ctx, t.Engine, c, func(c Conn) error {
		var err error
		dbs, err = lister.ListDatabases(ctx, c)
		return err
	})
	return dbs, err
}
