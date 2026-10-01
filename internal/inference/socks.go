package inference

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func parseSOCKS(raw string) (*url.URL, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	address := strings.TrimSpace(lines[0])
	if !strings.Contains(address, "://") {
		address = "socks5://" + address
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || (u.Scheme != "socks5" && u.Scheme != "socks5h") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid SOCKS5 configuration")
	}
	if _, _, err = net.SplitHostPort(u.Host); err != nil {
		return nil, fmt.Errorf("SOCKS5 requires host:port")
	}
	if len(lines) > 1 {
		if u.User != nil {
			return nil, fmt.Errorf("duplicate SOCKS5 credentials")
		}
		values := map[string]string{}
		for _, line := range lines[1:] {
			label, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			label = strings.ToLower(strings.TrimSpace(label))
			switch label {
			case "username", "login":
				label = "user"
			case "password":
				label = "pass"
			}
			if !ok || (label != "user" && label != "pass") {
				return nil, fmt.Errorf("invalid SOCKS5 credential labels")
			}
			if _, exists := values[label]; exists {
				return nil, fmt.Errorf("duplicate SOCKS5 credential label")
			}
			values[label] = strings.TrimSpace(value)
		}
		if len(values) != 2 || values["user"] == "" {
			return nil, fmt.Errorf("SOCKS5 user and pass required")
		}
		u.User = url.UserPassword(values["user"], values["pass"])
	}
	return u, nil
}
