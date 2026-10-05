package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file is a small, dependency-free implementation of MPD's text
// protocol (https://mpd.readthedocs.io/en/latest/protocol.html): just
// enough framing to send commands, read key/value responses, read binary
// chunks (album art), and block on "idle". client.go builds the TUI's
// domain-level calls on top of it.

// mpdError is an "ACK [code@line] {command} message" response — the server
// understood the request and refused it, as opposed to a broken connection.
type mpdError struct {
	Code    int
	Command string
	Message string
}

func (e *mpdError) Error() string {
	if e.Command != "" {
		return e.Command + ": " + e.Message
	}
	return e.Message
}

// MPD ACK codes the client cares about.
const (
	ackNoExist = 50
)

func parseAck(line string) *mpdError {
	// ACK [50@0] {play} No such song
	e := &mpdError{Message: line}
	rest := strings.TrimPrefix(line, "ACK ")
	if i := strings.Index(rest, "]"); strings.HasPrefix(rest, "[") && i > 0 {
		codeLine := rest[1:i]
		if at := strings.Index(codeLine, "@"); at > 0 {
			e.Code, _ = strconv.Atoi(codeLine[:at])
		}
		rest = strings.TrimSpace(rest[i+1:])
	}
	if strings.HasPrefix(rest, "{") {
		if i := strings.Index(rest, "}"); i > 0 {
			e.Command = rest[1:i]
			rest = strings.TrimSpace(rest[i+1:])
		}
	}
	e.Message = rest
	return e
}

// attr is one "key: value" line of a response, kept in order since MPD
// responses are flat streams where record boundaries are implied by
// particular keys (e.g. each "file:" starts a new song).
type attr struct {
	key, value string
}

// conn is a single protocol connection. It is not safe for concurrent use;
// Client serializes access to its command connection, and the idle watcher
// owns its own.
type conn struct {
	nc      net.Conn
	r       *bufio.Reader
	version string
}

// dialMPD opens a connection to addr — a "host:port" pair, or an absolute
// path / "@abstract" name for a unix socket — and authenticates if a
// password is given.
func dialMPD(addr, password string) (*conn, error) {
	network := "tcp"
	if strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, "@") {
		network = "unix"
	}
	nc, err := net.DialTimeout(network, addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c := &conn{nc: nc, r: bufio.NewReaderSize(nc, 64*1024)}

	nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	greeting, err := c.readLine()
	nc.SetReadDeadline(time.Time{})
	if err != nil {
		nc.Close()
		return nil, err
	}
	if !strings.HasPrefix(greeting, "OK MPD ") {
		nc.Close()
		return nil, fmt.Errorf("%s is not an MPD server (greeting %q)", addr, greeting)
	}
	c.version = strings.TrimPrefix(greeting, "OK MPD ")

	if password != "" {
		if _, err := c.cmd("password", password); err != nil {
			nc.Close()
			return nil, err
		}
	}
	// Larger binary chunks make album-art fetches a handful of round trips
	// instead of hundreds at the 8KiB default. Servers older than 0.22.4
	// don't know the command; that's harmless.
	c.cmd("binarylimit", "1048576")
	return c, nil
}

func (c *conn) Close() error { return c.nc.Close() }

func (c *conn) readLine() (string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(line, "\n"), nil
}

// quoteArg wraps an argument in double quotes, escaping backslashes and
// quotes, so values with spaces or special characters survive tokenizing.
func quoteArg(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func formatCommand(name string, args ...string) string {
	var b strings.Builder
	b.WriteString(name)
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(quoteArg(a))
	}
	b.WriteByte('\n')
	return b.String()
}

func (c *conn) write(s string) error {
	c.nc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := io.WriteString(c.nc, s)
	c.nc.SetWriteDeadline(time.Time{})
	return err
}

// readResponse reads key/value lines up to the terminating OK (or ACK). If
// binary is non-nil, any "binary: N" payload is appended to it.
func (c *conn) readResponse(binary *[]byte) ([]attr, error) {
	var attrs []attr
	for {
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		switch {
		case line == "OK" || line == "list_OK":
			if line == "OK" {
				return attrs, nil
			}
			continue
		case strings.HasPrefix(line, "ACK "):
			return nil, parseAck(line)
		}
		i := strings.Index(line, ": ")
		if i < 0 {
			// "Key:" with an empty value has no trailing space.
			if strings.HasSuffix(line, ":") {
				attrs = append(attrs, attr{key: strings.TrimSuffix(line, ":")})
				continue
			}
			return nil, fmt.Errorf("unexpected MPD response line %q", line)
		}
		k, v := line[:i], line[i+2:]
		if k == "binary" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("bad binary length %q", v)
			}
			buf := make([]byte, n+1) // payload plus trailing newline
			if _, err := io.ReadFull(c.r, buf); err != nil {
				return nil, err
			}
			if binary != nil {
				*binary = append(*binary, buf[:n]...)
			}
		}
		attrs = append(attrs, attr{key: k, value: v})
	}
}

func (c *conn) cmd(name string, args ...string) ([]attr, error) {
	if err := c.write(formatCommand(name, args...)); err != nil {
		return nil, err
	}
	return c.readResponse(nil)
}

// --- Client: a self-healing command connection ---

// Client owns one lazily-dialed command connection, guarded by a mutex so
// bubbletea's concurrently-running commands can share it. MPD drops idle
// clients after connection_timeout (60s by default), so a call that fails
// on a dead socket transparently redials and retries once.
type Client struct {
	addr     string
	password string

	mu sync.Mutex
	c  *conn

	// preMuteVolume backs the emulated mute toggle — MPD has no mute of its
	// own, so muting sets volume 0 and unmuting restores this.
	preMuteVolume int
	muted         bool
}

func NewClient(addr, password string) *Client {
	return &Client{addr: addr, password: password}
}

func (cl *Client) Addr() string { return cl.addr }

// run executes fn against a live connection, redialing and retrying once if
// the connection turned out to be dead. Server-side refusals (ACK) are not
// retried. Callers must not hold cl.mu.
func (cl *Client) run(fn func(c *conn) error) error {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if cl.c == nil {
			c, err := dialMPD(cl.addr, cl.password)
			if err != nil {
				return fmt.Errorf("connecting to %s: %w", cl.addr, err)
			}
			cl.c = c
		}
		err := fn(cl.c)
		var ack *mpdError
		if err == nil || errors.As(err, &ack) {
			return err
		}
		cl.c.Close()
		cl.c = nil
		if attempt > 0 {
			return err
		}
	}
}

func (cl *Client) cmd(name string, args ...string) ([]attr, error) {
	var out []attr
	err := cl.run(func(c *conn) error {
		var err error
		out, err = c.cmd(name, args...)
		return err
	})
	return out, err
}

// commandList sends cmds as one command_list_begin/command_list_end batch,
// chunked so a huge album or playlist doesn't hit MPD's per-list size cap
// (max_command_list_size, 2MiB by default).
func (cl *Client) commandList(cmds []string) error {
	const chunk = 500
	for len(cmds) > 0 {
		n := min(chunk, len(cmds))
		batch := "command_list_begin\n" + strings.Join(cmds[:n], "") + "command_list_end\n"
		err := cl.run(func(c *conn) error {
			if err := c.write(batch); err != nil {
				return err
			}
			_, err := c.readResponse(nil)
			return err
		})
		if err != nil {
			return err
		}
		cmds = cmds[n:]
	}
	return nil
}

// binary fetches a whole binary object (albumart / readpicture) by walking
// its offset until the reported size has been read.
func (cl *Client) binary(name, uri string) ([]byte, error) {
	var data []byte
	err := cl.run(func(c *conn) error {
		data = data[:0]
		for {
			attrs, err := c.readBinaryChunk(name, uri, len(data), &data)
			if err != nil {
				return err
			}
			size := -1
			chunk := 0
			for _, a := range attrs {
				switch a.key {
				case "size":
					size, _ = strconv.Atoi(a.value)
				case "binary":
					chunk, _ = strconv.Atoi(a.value)
				}
			}
			if chunk == 0 || size < 0 || len(data) >= size {
				return nil
			}
		}
	})
	return data, err
}

func (c *conn) readBinaryChunk(name, uri string, offset int, data *[]byte) ([]attr, error) {
	if err := c.write(formatCommand(name, uri, strconv.Itoa(offset))); err != nil {
		return nil, err
	}
	return c.readResponse(data)
}

// --- idle ---

// Watch dials a dedicated connection and blocks in "idle", delivering each
// batch of changed subsystem names to onChange until the connection fails,
// at which point it returns the error (the caller reconnects).
func (cl *Client) Watch(onConnect func(), onChange func(subsystems []string)) error {
	c, err := dialMPD(cl.addr, cl.password)
	if err != nil {
		return err
	}
	defer c.Close()
	onConnect()
	for {
		attrs, err := c.cmd("idle")
		if err != nil {
			return err
		}
		var changed []string
		for _, a := range attrs {
			if a.key == "changed" {
				changed = append(changed, a.value)
			}
		}
		if len(changed) > 0 {
			onChange(changed)
		}
	}
}

// --- filter expressions ---

// filterEq builds an MPD filter expression matching tag exactly, e.g.
// (AlbumArtist == "Oasis"). The expression is itself passed as a quoted
// argument, so the value is escaped here once for the filter grammar and
// again by quoteArg for the command line.
func filterEq(tag, value string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return "(" + tag + ` == "` + r.Replace(value) + `")`
}

func filterAnd(exprs ...string) string {
	if len(exprs) == 1 {
		return exprs[0]
	}
	return "(" + strings.Join(exprs, " AND ") + ")"
}
