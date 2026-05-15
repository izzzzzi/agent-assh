package serverinfo

import (
	"errors"
	"strings"
)

type Info struct {
	Host     string
	IPv6     string
	User     string
	Password string
}

func Parse(input string) (Info, error) {
	var info Info
	var active string
	var passwordLines []string

	for _, raw := range strings.Split(input, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		key, value, ok := splitLabel(line)
		if !ok {
			if active == "password" {
				passwordLines = append(passwordLines, stripCopyMarker(line))
			}
			continue
		}

		field := fieldName(key)
		if field == "" {
			if active == "password" {
				passwordLines = append(passwordLines, stripCopyMarker(line))
			}
			continue
		}
		active = field

		switch field {
		case "host":
			info.Host = stripCopyMarker(value)
		case "ipv6":
			info.IPv6 = stripCopyMarker(value)
		case "user":
			info.User = stripCopyMarker(value)
		case "password":
			passwordLines = []string{stripCopyMarker(value)}
		}
	}

	info.Password = decodeEscapedNewlines(strings.TrimSpace(strings.Join(passwordLines, "\n")))
	if info.Host == "" {
		return Info{}, errors.New("server info is missing IPv4 host")
	}
	if info.User == "" {
		return Info{}, errors.New("server info is missing user")
	}
	if info.Password == "" {
		return Info{}, errors.New("server info is missing password")
	}
	return info, nil
}

func splitLabel(line string) (string, string, bool) {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), true
}

func fieldName(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "ё", "е")
	switch normalized {
	case "ipv4", "ipv4-адрес сервера", "ipv4 адрес сервера", "server ipv4", "ipv4 address", "host", "hostname":
		return "host"
	case "ipv6", "ipv6-адрес сервера", "ipv6 адрес сервера", "server ipv6", "ipv6 address":
		return "ipv6"
	case "пользователь", "user", "username", "login":
		return "user"
	case "пароль", "password", "pass":
		return "password"
	default:
		return ""
	}
}

func stripCopyMarker(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "copy icon")
	value = strings.TrimSuffix(value, "copy")
	return strings.TrimSpace(value)
}

func decodeEscapedNewlines(value string) string {
	return strings.ReplaceAll(value, `\n`, "\n")
}
