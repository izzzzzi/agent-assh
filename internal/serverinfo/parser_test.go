package serverinfo

import "testing"

func TestParseRussianProviderBlock(t *testing.T) {
	block := `Информация о сервере
Тарифный план: Cloud Pro Intel| NL-2 v.2
Дата открытия: 2025-01-12
IPv4-адрес сервера: 203.0.113.10 copy icon
IPv6-адрес сервера: 2001:db8::51 copy icon
Пользователь: root copy icon
Пароль: example\npassword$1 copy icon`

	info, err := Parse(block)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if info.Host != "203.0.113.10" {
		t.Fatalf("Host=%q want IPv4", info.Host)
	}
	if info.IPv6 != "2001:db8::51" {
		t.Fatalf("IPv6=%q", info.IPv6)
	}
	if info.User != "root" {
		t.Fatalf("User=%q want root", info.User)
	}
	if info.Password != "example\npassword$1" {
		t.Fatalf("Password=%q want escaped newline decoded", info.Password)
	}
}

func TestParseFallsBackToIPv6Host(t *testing.T) {
	block := `IPv6-адрес сервера: 2001:db8::51 copy icon
Пользователь: root copy icon
Пароль: example copy icon`

	info, err := Parse(block)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if info.Host != "2001:db8::51" {
		t.Fatalf("Host=%q want IPv6 fallback", info.Host)
	}
	if info.IPv6 != "2001:db8::51" {
		t.Fatalf("IPv6=%q", info.IPv6)
	}
}

func TestParseMultilinePasswordUntilNextKnownLabel(t *testing.T) {
	block := `Host: 203.0.113.10
User: root
Password: example line
continued line
IPv6: 2001:db8::1`

	info, err := Parse(block)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if info.Password != "example line\ncontinued line" {
		t.Fatalf("Password=%q", info.Password)
	}
	if info.IPv6 != "2001:db8::1" {
		t.Fatalf("IPv6=%q", info.IPv6)
	}
}

func TestParseStopsPasswordAtUnknownLabelAndReadsPort(t *testing.T) {
	block := `Host: 203.0.113.10
User: root
Password: backupcopy
Panel URL: https://panel.example
SSH Port: 2222`

	info, err := Parse(block)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if info.Password != "backupcopy" {
		t.Fatalf("Password=%q, want backupcopy", info.Password)
	}
	if info.Port != 2222 {
		t.Fatalf("Port=%d, want 2222", info.Port)
	}
}

func TestParseRequiresHostUserAndPassword(t *testing.T) {
	_, err := Parse("User: root\nPassword: secret")
	if err == nil {
		t.Fatal("expected missing host error")
	}
}
