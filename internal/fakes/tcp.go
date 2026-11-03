package fakes

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"
)

// TCPOptions shapes the fake TCP agent.
type TCPOptions struct {
	Length     bool        // 4-byte big-endian length framing instead of a terminator
	Terminator string      // default "\n"
	Raw        bool        // the request is plain text, not JSON (informational)
	Silent     bool        // read the request, send nothing
	NoTerm     bool        // reply without a terminator, then wait (for idle reads)
	TLS        *tls.Config // serve TLS
}

// TCP is a line- or length-framed agent on a loopback port.
type TCP struct {
	ln  net.Listener
	opt TCPOptions
	a   *Agent
	mu  sync.Mutex
	got []string
	wg  sync.WaitGroup
}

// NewTCP starts the fake.
func NewTCP(a *Agent, opt TCPOptions) *TCP {
	if opt.Terminator == "" {
		opt.Terminator = "\n"
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	if opt.TLS != nil {
		ln = tls.NewListener(ln, opt.TLS)
	}
	f := &TCP{ln: ln, opt: opt, a: a}
	go f.serve()
	return f
}

// URL is the tcp:// address.
func (f *TCP) URL() string { return "tcp://" + f.ln.Addr().String() }

// Close stops the listener and waits for handlers.
func (f *TCP) Close() { _ = f.ln.Close(); f.wg.Wait() }

// Received lists the request texts read so far.
func (f *TCP) Received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func (f *TCP) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.wg.Add(1)
		go func() { defer f.wg.Done(); f.handle(c) }()
	}
}

func (f *TCP) handle(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(c)
	var req []byte
	if f.opt.Length {
		var hdr [4]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return
		}
		req = make([]byte, binary.BigEndian.Uint32(hdr[:]))
		if _, err := io.ReadFull(br, req); err != nil {
			return
		}
	} else {
		term := []byte(f.opt.Terminator)
		for !bytes.HasSuffix(req, term) {
			b, err := br.ReadByte()
			if err != nil {
				return
			}
			req = append(req, b)
		}
		req = bytes.TrimSuffix(req, term)
	}
	f.mu.Lock()
	f.got = append(f.got, string(req))
	f.mu.Unlock()
	if f.opt.Silent {
		time.Sleep(300 * time.Millisecond)
		return
	}
	reply := f.a.Reply(string(req))
	switch {
	case f.opt.Length:
		out := make([]byte, 4, 4+len(reply))
		binary.BigEndian.PutUint32(out, uint32(len(reply)))
		_, _ = c.Write(append(out, reply...))
	case f.opt.NoTerm:
		_, _ = c.Write(reply)
		time.Sleep(1500 * time.Millisecond)
	default:
		_, _ = c.Write(append(reply, []byte(f.opt.Terminator)...))
		time.Sleep(50 * time.Millisecond)
	}
}
