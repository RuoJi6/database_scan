package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

type directDialer struct {
	net.Dialer
}

func Direct(timeout time.Duration) Dialer {
	return &directDialer{Dialer: net.Dialer{Timeout: timeout}}
}

func FromURL(raw string, timeout time.Duration) (Dialer, error) {
	if raw == "" {
		return Direct(timeout), nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse proxy url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h":
		return socks5Dialer(u, timeout)
	case "http", "https":
		return &httpConnectDialer{proxyURL: u, timeout: timeout}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
	}
}

func socks5Dialer(u *url.URL, timeout time.Duration) (Dialer, error) {
	var auth *xproxy.Auth
	if u.User != nil {
		pass, _ := u.User.Password()
		auth = &xproxy.Auth{User: u.User.Username(), Password: pass}
	}
	d, err := xproxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{Timeout: timeout})
	if err != nil {
		return nil, err
	}
	ctxDialer, ok := d.(xproxy.ContextDialer)
	if !ok {
		return nil, errors.New("socks5 dialer does not support context")
	}
	return &socks5ContextDialer{dialer: ctxDialer}, nil
}

type socks5ContextDialer struct {
	dialer xproxy.ContextDialer
}

func (d *socks5ContextDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.dialer.DialContext(ctx, network, address)
}

type httpConnectDialer struct {
	proxyURL *url.URL
	timeout  time.Duration
}

func (d *httpConnectDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("http proxy only supports tcp, got %s", network)
	}
	nd := net.Dialer{Timeout: d.timeout}
	conn, err := nd.DialContext(ctx, "tcp", d.proxyURL.Host)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Host: address},
		Host:   address,
		Header: make(http.Header),
	}
	if d.proxyURL.User != nil {
		pass, _ := d.proxyURL.User.Password()
		token := base64.StdEncoding.EncodeToString([]byte(d.proxyURL.User.Username() + ":" + pass))
		req.Header.Set("Proxy-Authorization", "Basic "+token)
	}
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("http proxy CONNECT failed: %s", resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}
