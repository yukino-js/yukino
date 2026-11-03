package remote

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var remoteAddressPattern = regexp.MustCompile(`^(?:\[([^\]]+)\]|([^:]*))(?::([0-9]+))?$`)
var remotePortPattern = regexp.MustCompile(`^[0-9]+$`)

func ParseRemoteAddress(address string, defaultPort int, allowEphemeral bool) (string, int, error) {
	value := strings.TrimSpace(address)
	host, portText := "", ""
	if remotePortPattern.MatchString(value) {
		portText = value
	} else {
		match := remoteAddressPattern.FindStringSubmatch(value)
		if match == nil {
			return "", 0, fmt.Errorf("Invalid remote address; use port, host:port or [IPv6]:port")
		}
		host = match[1]
		if host == "" {
			host = match[2]
		}
		portText = match[3]
	}
	port := defaultPort
	if portText != "" {
		parsed, err := strconv.Atoi(portText)
		if err != nil {
			port = -1
		} else {
			port = parsed
		}
	}
	minimum := 1
	if allowEphemeral {
		minimum = 0
	}
	if port < minimum || port > 65535 {
		return "", 0, fmt.Errorf("Remote port must be an integer between %d and 65535", minimum)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return host, port, nil
}
func createAccessToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}
func authorizeWebSocket(r *http.Request, path, token string) (int, string) {
	if r.URL.Path != path {
		return http.StatusNotFound, "Not Found"
	}
	provided := r.URL.Query().Get("token")
	if provided == "" || len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
		return http.StatusUnauthorized, "Unauthorized"
	}
	if origin, present := r.Header["Origin"]; present {
		if len(origin) != 1 || r.Host == "" {
			return http.StatusForbidden, "Forbidden"
		}
		parsed, err := url.Parse(origin[0])
		if err != nil || parsed.Scheme != "http" || canonicalOriginHost(parsed) != r.Host {
			return http.StatusForbidden, "Forbidden"
		}
	}
	return 0, ""
}

func canonicalOriginHost(parsed *url.URL) string {
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "80" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host
}
func (s *Server) listenAddress() (string, error) {
	host, port, err := ParseRemoteAddress(s.addr, 18888, false)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
