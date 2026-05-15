package proxy

import (
	"encoding/base64"
	"testing"
	"time"

	xproxy "golang.org/x/net/proxy"
)

func TestFromURLUnsupportedScheme(t *testing.T) {
	if _, err := FromURL("ftp://127.0.0.1:8080", time.Second); err == nil {
		t.Fatal("expected unsupported scheme error")
	}
}

func TestHTTPConnectDialerParse(t *testing.T) {
	d, err := FromURL("http://user:pass@127.0.0.1:8080", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	httpDialer, ok := d.(*httpConnectDialer)
	if !ok {
		t.Fatalf("expected httpConnectDialer, got %T", d)
	}
	if got := httpDialer.proxyURL.User.Username(); got != "user" {
		t.Fatalf("unexpected proxy user: %s", got)
	}
	pass, _ := httpDialer.proxyURL.User.Password()
	gotToken := base64.StdEncoding.EncodeToString([]byte(httpDialer.proxyURL.User.Username() + ":" + pass))
	wantToken := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if gotToken != wantToken {
		t.Fatalf("unexpected basic auth token: %s", gotToken)
	}
}

func TestSOCKS5ProxyAuthParse(t *testing.T) {
	d, err := FromURL("socks5://user:pass@127.0.0.1:1080", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	socksDialer, ok := d.(*socks5ContextDialer)
	if !ok {
		t.Fatalf("expected socks5ContextDialer, got %T", d)
	}
	if socksDialer.dialer == nil {
		t.Fatal("expected non-nil socks5 context dialer")
	}
	auth := &xproxy.Auth{User: "user", Password: "pass"}
	if auth.User != "user" || auth.Password != "pass" {
		t.Fatalf("unexpected auth: %#v", auth)
	}
}
