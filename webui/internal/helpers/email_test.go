package helpers

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// smtpCapture is what the fake server recorded from one client session.
type smtpCapture struct {
	commands []string // every non-DATA line the client sent (one per command)
	data     []string // DATA payload lines (dot-stuffing undone)
	closed   string   // why the session ended: "quit" | "violation" | "eof"
}

// fakeSMTPServer implements just enough of RFC 5321 to drive sendSMTPMessage
// end to end on loopback: greeting, EHLO (no STARTTLS advertised → the client
// stays on plain TCP, matching the non-465 branch), MAIL/RCPT/DATA, QUIT.
// Empty smtpUser means no AUTH is attempted, so the session is a clean
// transport probe.
//
// A strict server disconnects when the peer sends something it cannot parse —
// exactly what a CRLF-injected RCPT argument produces (the injected fragment
// arrives as an unknown command). Recording the fragment and cutting the
// connection mirrors that behaviour AND un-hangs the test client, whose
// request/response desync would otherwise wait forever.
func fakeSMTPServer(t *testing.T) (addr string, captured <-chan smtpCapture) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	ch := make(chan smtpCapture, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		writeLine := func(s string) {
			_, _ = w.WriteString(s + "\r\n")
			_ = w.Flush()
		}
		finish := func(why string, cmds []string, data []string) {
			ch <- smtpCapture{commands: cmds, data: data, closed: why}
		}
		writeLine("220 examvan-test ESMTP")
		var (
			cmds   []string
			data   []string
			inData bool
		)
		for {
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			line, err := r.ReadString('\n')
			if err != nil {
				// Client went away mid-session (desync, deadline, or our
				// earlier close) — report whatever was captured so far.
				if inData {
					finish("eof", cmds, data)
				} else {
					finish("eof", cmds, data)
				}
				return
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if inData {
				if trimmed == "." {
					writeLine("250 OK queued")
					inData = false
					continue
				}
				// Dot-stuffing undo (RFC 5321 4.5.2).
				data = append(data, strings.TrimPrefix(trimmed, "."))
				continue
			}
			cmds = append(cmds, trimmed)
			upper := strings.ToUpper(trimmed)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				writeLine("250-TEST greets you")
				writeLine("250 OK")
			case strings.HasPrefix(upper, "MAIL FROM"):
				writeLine("250 OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				writeLine("250 OK")
			case strings.HasPrefix(upper, "DATA"):
				writeLine("354 send data, end with .")
				inData = true
			case strings.HasPrefix(upper, "QUIT"):
				writeLine("221 bye")
				finish("quit", cmds, data)
				return
			default:
				// Strict server: a command we cannot parse is a protocol
				// violation (injected fragment) — record it and cut the
				// connection without a reply.
				finish("violation", cmds, data)
				return
			}
		}
	}()
	return ln.Addr().String(), ch
}

func waitCapture(t *testing.T, ch <-chan smtpCapture) smtpCapture {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("fake SMTP server never reported a session end — client hung")
		return smtpCapture{}
	}
}

// TestSendSMTPMessageDeliversHappyPath pins the transport contract for a
// normal, well-formed address: the message goes out, carries the recipient
// header, and the envelope RCPT holds the address.
func TestSendSMTPMessageDeliversHappyPath(t *testing.T) {
	addr, ch := fakeSMTPServer(t)
	host, port, _ := net.SplitHostPort(addr)

	err := sendSMTPMessage(host, port, "", "", "EXAMVAN", "budi@sekolah.sch.id", "Kode OTP", "KODE: 123456")
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	got := waitCapture(t, ch)
	if got.closed != "quit" {
		t.Fatalf("session ended via %q, want clean quit", got.closed)
	}
	found := false
	for _, line := range got.data {
		if strings.EqualFold(line, "to: budi@sekolah.sch.id") {
			found = true
		}
	}
	if !found {
		t.Errorf("DATA missing To header — got:\n%s", strings.Join(got.data, "\n"))
	}
	envOK := false
	for _, cmd := range got.commands {
		if strings.EqualFold(cmd, "rcpt to:<budi@sekolah.sch.id>") {
			envOK = true
		}
	}
	if !envOK {
		t.Errorf("envelope missing RCPT TO — commands:\n%s", strings.Join(got.commands, "\n"))
	}
}

// TestSendSMTPMessageStripsCRLFInjection is the regression test for the
// 14 Nov 2026 review finding: the email field (registration / user management)
// was validated with only `contains @` and `contains .`, so a value like
// `a@b.<CRLF>Bcc: attacker@x` reached sendSMTPMessage RAW — once into the
// To: header and once into the envelope RCPT command, where the injected line
// arrives at the server as an SMTP command of the attacker's choosing. The
// transport is a security boundary: it strips CR/LF/control characters from
// every client-supplied interpolation BEFORE any bytes leave, regardless of
// how well the entry points validate.
//
// Against the pre-fix code this test FAILS: the injected fragment arrives at
// the server as an unknown command (a "violation" close), and the DATA
// payload carries a standalone `Bcc:` line that never existed in a legitimate
// conversation. After the fix the CR/LF are gone — the residue can only ride
// INSIDE one header line, never as its own line, so it cannot be an injected
// header or command.
func TestSendSMTPMessageStripsCRLFInjection(t *testing.T) {
	addr, ch := fakeSMTPServer(t)
	host, port, _ := net.SplitHostPort(addr)

	malicious := "a@b.\r\nBcc: attacker@x.example\r\nSubject: forged\r\n"
	// The send itself may fail downstream (a strict server closes on the
	// violation; the sanitized residue is not a valid mailbox) — fine and even
	// desirable. What must NEVER happen is the injection surviving the wire.
	_ = sendSMTPMessage(host, port, "", "", "EXAMVAN", malicious, "Kode OTP", "KODE: 123456")

	got := waitCapture(t, ch)
	if got.closed == "violation" {
		t.Errorf("SMTP command injection survived the transport — the server saw an injected command: %q",
			got.commands[len(got.commands)-1])
	}
	for _, line := range got.data {
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "bcc:") {
			t.Errorf("header injection survived the transport — standalone Bcc: line in DATA:\n%s", line)
		}
		if low == "subject: forged" {
			t.Errorf("injected header value survived as its own header line:\n%s", line)
		}
	}
	// Every command line must be a well-formed SMTP verb — no orphan
	// injection fragments masquerading as commands.
	for _, cmd := range got.commands {
		u := strings.ToUpper(cmd)
		if !strings.HasPrefix(u, "EHLO") && !strings.HasPrefix(u, "HELO") &&
			!strings.HasPrefix(u, "MAIL") && !strings.HasPrefix(u, "RCPT") &&
			!strings.HasPrefix(u, "DATA") && !strings.HasPrefix(u, "QUIT") {
			t.Errorf("malformed/unknown SMTP command reached the server: %q", cmd)
		}
	}
}
