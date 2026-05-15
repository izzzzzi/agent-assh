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

func TestParseRequiresHostUserAndPassword(t *testing.T) {
	_, err := Parse("User: root\nPassword: secret")
	if err == nil {
		t.Fatal("expected missing host error")
	}
}
