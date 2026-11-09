package console

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Proxy struct {
	tokens              *Tokens
	upgrader            websocket.Upgrader
	trustForwardedProto func(*http.Request) bool
}

func NewProxy(tokens *Tokens, trustProxy bool, trustedProxy func(*http.Request) bool) *Proxy {
	p := &Proxy{tokens: tokens}
	if trustProxy {
		p.trustForwardedProto = trustedProxy
	}
	p.upgrader = websocket.Upgrader{
		ReadBufferSize:  32 * 1024,
		WriteBufferSize: 32 * 1024,
		Subprotocols:    []string{"binary"},
		CheckOrigin: func(r *http.Request) bool {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			return origin != "" && sameOrigin(r, p.trustForwardedProto)
		},
	}
	return p
}

func sameOrigin(r *http.Request, trustForwardedProto func(*http.Request) bool) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil ||
		origin.Opaque != "" || origin.RawQuery != "" || origin.Fragment != "" ||
		(origin.Path != "" && origin.Path != "/") {
		return false
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if trustForwardedProto != nil && trustForwardedProto(r) {
		forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
		if strings.EqualFold(forwarded, "https") || strings.EqualFold(forwarded, "http") {
			scheme = strings.ToLower(forwarded)
		}
	}
	if !strings.EqualFold(origin.Scheme, scheme) {
		return false
	}
	return sameAuthority(origin.Host, r.Host, scheme)
}

func sameAuthority(a, b, scheme string) bool {
	hostA, portA, okA := authorityParts(a, scheme)
	hostB, portB, okB := authorityParts(b, scheme)
	return okA && okB && hostA == hostB && portA == portB
}

func authorityParts(authority, scheme string) (string, string, bool) {
	u, err := url.Parse("//" + authority)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return "", "", false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	port := u.Port()
	if port == "" {
		if strings.EqualFold(scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host, port, true
}

func (p *Proxy) Upgrade(w http.ResponseWriter, r *http.Request, backend *Backend) error {
	if backend == nil {
		return context.Canceled
	}
	// Close the backend even when the WebSocket handshake itself is rejected
	// (for example, a missing or cross-origin Origin header).
	defer backend.Close()
	ws, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	ws.SetReadLimit(2 * 1024 * 1024)
	_ = ws.SetReadDeadline(time.Now().Add(75 * time.Second))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(75 * time.Second))
	})

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var once sync.Once
	var writeMu sync.Mutex
	closeBoth := func() {
		once.Do(func() {
			cancel()
			_ = ws.Close()
			_ = backend.Close()
		})
	}
	writeMessage := func(messageType int, payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = ws.SetWriteDeadline(time.Now().Add(30 * time.Second))
		return ws.WriteMessage(messageType, payload)
	}

	errCh := make(chan error, 3)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := writeMessage(websocket.PingMessage, nil); err != nil {
					errCh <- err
					closeBoth()
					return
				}
			}
		}
	}()
	go func() {
		for {
			messageType, payload, err := ws.ReadMessage()
			if err != nil {
				errCh <- err
				closeBoth()
				return
			}
			if messageType != websocket.BinaryMessage && messageType != websocket.TextMessage {
				continue
			}
			_ = ws.SetReadDeadline(time.Now().Add(75 * time.Second))
			_ = backend.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if err := writeAll(backend.conn, payload); err != nil {
				errCh <- err
				closeBoth()
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := backend.conn.Read(buf)
			if n > 0 {
				if err := writeMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					errCh <- err
					closeBoth()
					return
				}
			}
			if err != nil {
				errCh <- err
				closeBoth()
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return nil
		}
		return err
	}
}

func writeAll(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		n, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		payload = payload[n:]
	}
	return nil
}
