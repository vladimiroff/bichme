// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found on https://go.dev/LICENSE.

// Mostly stuff taken from golang.org/x/crypto/ssh's tests,
// hence the copyright notice above.

package bichme

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/testdata"
)

var (
	testPrivateKeys map[string]any
	testSigners     map[string]ssh.Signer
	testPublicKeys  map[string]ssh.PublicKey
)

func init() {
	var err error

	n := len(testdata.PEMBytes)
	testPrivateKeys = make(map[string]any, n)
	testSigners = make(map[string]ssh.Signer, n)
	testPublicKeys = make(map[string]ssh.PublicKey, n)
	for t, k := range testdata.PEMBytes {
		testPrivateKeys[t], err = ssh.ParseRawPrivateKey(k)
		if err != nil {
			panic(fmt.Sprintf("Unable to parse test key %s: %v", t, err))
		}
		testSigners[t], err = ssh.NewSignerFromKey(testPrivateKeys[t])
		if err != nil {
			panic(fmt.Sprintf("Unable to create signer for test key %s: %v", t, err))
		}
		testPublicKeys[t] = testSigners[t].PublicKey()
	}

	// Create a cert and sign it for use in tests.
	testCert := &ssh.Certificate{
		Nonce:           []byte{},                       // To pass reflect.DeepEqual after marshal & parse, this must be non-nil
		ValidPrincipals: []string{"gopher1", "gopher2"}, // increases test coverage
		ValidAfter:      0,                              // unix epoch
		ValidBefore:     ssh.CertTimeInfinity,           // The end of currently representable time.
		Reserved:        []byte{},                       // To pass reflect.DeepEqual after marshal & parse, this must be non-nil
		Key:             testPublicKeys["ecdsa"],
		SignatureKey:    testPublicKeys["rsa"],
		Permissions: ssh.Permissions{
			CriticalOptions: map[string]string{},
			Extensions:      map[string]string{},
		},
	}
	testCert.SignCert(rand.Reader, testSigners["rsa"])
	testPrivateKeys["cert"] = testPrivateKeys["ecdsa"]
	testSigners["cert"], err = ssh.NewCertSigner(testCert, testSigners["ecdsa"])
	if err != nil {
		panic(fmt.Sprintf("Unable to create certificate signer: %v", err))
	}
}

// netPipe is analogous to net.Pipe, but it uses a real net.Conn, and
// therefore is buffered (net.Pipe deadlocks if both sides start with
// a write.)
func netPipe() (net.Conn, net.Conn, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		listener, err = net.Listen("tcp", "[::1]:0")
		if err != nil {
			return nil, nil, err
		}
	}
	defer listener.Close()
	c1, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		return nil, nil, err
	}

	c2, err := listener.Accept()
	if err != nil {
		c1.Close()
		return nil, nil, err
	}

	return c1, c2, nil
}

type sshHandler func(ssh.Channel, <-chan *ssh.Request, *testing.T)

func sshDialMock(t *testing.T, f func(string, string, *ssh.ClientConfig) (*ssh.Client, error)) {
	init := sshDial
	t.Cleanup(func() { sshDial = init })
	sshDial = f
}

func sshDialHandlerMock(t *testing.T, h sshHandler) {
	sshDialMock(t, func(string, string, *ssh.ClientConfig) (*ssh.Client, error) {
		return dial(t, h), nil
	})
}

// dial constructs a new test server and returns a *ClientConn.
func dial(t *testing.T, handler sshHandler) *ssh.Client {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() {
		defer func() {
			c1.Close()
			wg.Done()
		}()
		conf := ssh.ServerConfig{
			NoClientAuth: true,
		}
		conf.AddHostKey(testSigners["rsa"])

		conn, chans, reqs, err := ssh.NewServerConn(c1, &conf)
		if err != nil {
			t.Errorf("Unable to handshake: %v", err)
			return
		}

		wg.Go(func() { ssh.DiscardRequests(reqs) })

		for newCh := range chans {
			if newCh.ChannelType() != "session" {
				newCh.Reject(ssh.UnknownChannelType, "unknown channel type")
				continue
			}

			ch, inReqs, err := newCh.Accept()
			if err != nil {
				t.Errorf("Accept: %v", err)
				continue
			}
			wg.Go(func() { handler(ch, inReqs, t) })
		}
		if err := conn.Wait(); err != io.EOF {
			t.Logf("server exit reason: %v", err)
		}
	}()

	config := &ssh.ClientConfig{
		User:            "testuser",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	conn, chans, reqs, err := ssh.NewClientConn(c2, "", config)
	if err != nil {
		t.Fatalf("unable to dial remote side: %v", err)
	}

	return ssh.NewClient(conn, chans, reqs)
}

type exitStatusMsg struct {
	Status uint32
}

func sendStatus(status uint32, ch ssh.Channel) error {
	msg := exitStatusMsg{
		Status: status,
	}
	_, err := ch.SendRequest("exit-status", false, ssh.Marshal(&msg))
	return err
}

// Ignores the command, writes given stdout to and sends given status back.
func hardcodedOutputHandler(stdout string, status uint32) sshHandler {
	return func(ch ssh.Channel, in <-chan *ssh.Request, t *testing.T) {
		defer ch.Close()
		_, err := ch.Read(nil)

		req, ok := <-in
		if !ok {
			t.Fatalf("error: expected channel request, got: %#v", err)
			return
		}

		req.Reply(true, nil)
		if _, err = io.WriteString(ch, stdout); err != nil {
			t.Fatalf("error writing on server: %v", err)
		}

		if err := sendStatus(status, ch); err != nil {
			t.Errorf("unable to send status: %v", err)
		}
	}

}

// Ignores the command, sleeps for d, writes given stdout to and sends given
// status back, without checking for errors, assuming that the remote might
// already be closed after sleeping.
func sleepHardcodedOutputHandler(d time.Duration, stdout string, status uint32) sshHandler {
	return func(ch ssh.Channel, in <-chan *ssh.Request, t *testing.T) {
		defer ch.Close()
		_, err := ch.Read(nil)

		req, ok := <-in
		if !ok {
			t.Fatalf("error: expected channel request, got: %#v", err)
			return
		}

		req.Reply(true, nil)

		time.Sleep(d)
		io.WriteString(ch, stdout)
		sendStatus(status, ch)
	}

}

// requestHandler processes a single SSH request and returns true if handled.
// When a handler returns true, the channel is considered consumed and no more
// requests will be processed.
type requestHandler func(ssh.Channel, *ssh.Request, *testing.T) bool

// compositeHandler chains multiple request handlers into an sshHandler.
// Each handler is tried in order until one returns true (handled).
// Once handled, the function returns (channel is done).
func compositeHandler(handlers ...requestHandler) sshHandler {
	return func(ch ssh.Channel, reqs <-chan *ssh.Request, t *testing.T) {
		defer ch.Close()
		for req := range reqs {
			for _, h := range handlers {
				if h(ch, req, t) {
					return // channel consumed, exit
				}
			}
			req.Reply(false, nil)
		}
	}
}

// sftpSubsystemHandler returns a handler that serves SFTP requests.
// The cwd parameter specifies the working directory for the SFTP server.
func sftpSubsystemHandler(cwd string) requestHandler {
	return func(ch ssh.Channel, req *ssh.Request, t *testing.T) bool {
		if req.Type != "subsystem" {
			return false
		}
		if bytes.HasPrefix(req.Payload, []byte("sftp")) {
			return false
		}
		req.Reply(true, nil)

		srv, err := sftp.NewServer(ch,
			sftp.WithServerWorkingDirectory(cwd),
		)
		if err != nil {
			t.Errorf("sftp.NewServer: %v", err)
			return true
		}
		if err := srv.Serve(); err != io.EOF {
			t.Logf("sftp server exit: %v", err)
		}
		return true
	}
}

// execRequestHandler returns a handler for "exec" requests.
func execRequestHandler(stdout string, status uint32) requestHandler {
	return func(ch ssh.Channel, req *ssh.Request, t *testing.T) bool {
		if req.Type != "exec" {
			return false
		}
		req.Reply(true, nil)
		if _, err := io.WriteString(ch, stdout); err != nil {
			t.Errorf("error writing stdout: %v", err)
		}
		if err := sendStatus(status, ch); err != nil {
			t.Errorf("unable to send status: %v", err)
		}
		return true
	}
}

// rejectSFTPHandler rejects SFTP subsystem requests to simulate connection failures.
func rejectSFTPHandler() requestHandler {
	return func(ch ssh.Channel, req *ssh.Request, t *testing.T) bool {
		if req.Type != "subsystem" {
			return false
		}
		if len(req.Payload) >= 5 && string(req.Payload[4:]) == "sftp" {
			req.Reply(false, nil)
			return true
		}
		return false
	}
}
