package remote

import (
	"net/http/httptest"
	"testing"
)

func TestRemoteAddressForms(t *testing.T) {
	for _, tc := range []struct {
		in, host string
		port     int
	}{{"", "127.0.0.1", 18888}, {"9000", "127.0.0.1", 9000}, {"localhost", "localhost", 18888}, {":9000", "127.0.0.1", 9000}, {"[::1]:9000", "::1", 9000}} {
		host, port, err := ParseRemoteAddress(tc.in, 18888, false)
		if err != nil || host != tc.host || port != tc.port {
			t.Fatal(tc, host, port, err)
		}
	}
	for _, input := range []string{"0", "65536", "host:", "::1", "host:-1"} {
		if _, _, err := ParseRemoteAddress(input, 18888, false); err == nil {
			t.Fatal(input)
		}
	}
	if _, port, err := ParseRemoteAddress("0", 18888, true); err != nil || port != 0 {
		t.Fatal(port, err)
	}
}
func TestWebSocketTokenAndOrigin(t *testing.T) {
	token := createAccessToken()
	if len(token) != 43 || token == createAccessToken() {
		t.Fatal("invalid access token")
	}
	for _, tc := range []struct {
		path, origin string
		want         int
	}{{"/ws?token=" + token, "", 0}, {"/ws?token=" + token, "http://localhost:18888", 0}, {"/other?token=" + token, "", 404}, {"/ws", "", 401}, {"/ws?token=wrong", "", 401}, {"/ws?token=" + token, "https://localhost:18888", 403}, {"/ws?token=" + token, "http://other", 403}, {"/ws?token=" + token, "null", 403}} {
		r := httptest.NewRequest("GET", "http://localhost:18888"+tc.path, nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		status, _ := authorizeWebSocket(r, "/ws", token)
		if status != tc.want {
			t.Fatal(tc, status)
		}
	}
}
