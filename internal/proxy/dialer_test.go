package proxy

import (
	"testing"
	"time"
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
}
