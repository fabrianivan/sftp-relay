package secrets

import "testing"

func TestResolvePasswordPrecedence(t *testing.T) {
	store := NewMemory()
	acct := Account("u", "h", 22)

	if pw, src := ResolvePassword(store, "source", "u", "h", 22, "plain"); pw != "plain" || src != FromConfig {
		t.Fatalf("got %q from %s", pw, src)
	}
	_ = store.Set(acct, "from-keychain")
	if pw, src := ResolvePassword(store, "source", "u", "h", 22, "plain"); pw != "from-keychain" || src != FromKeychain {
		t.Fatalf("keychain should win over config, got %q from %s", pw, src)
	}
	t.Setenv("SFTP_RELAY_SOURCE_PASSWORD", "from-env")
	if pw, src := ResolvePassword(store, "source", "u", "h", 22, "plain"); pw != "from-env" || src != FromEnv {
		t.Fatalf("env should win, got %q from %s", pw, src)
	}
	if _, src := ResolvePassword(nil, "target", "u", "h", 22, ""); src != FromNone {
		t.Fatalf("got %s", src)
	}
}

func TestAccountIsSanitized(t *testing.T) {
	got := Account("bob smith", `h"; rm -rf /`, 2222)
	for _, bad := range []string{" ", `"`, ";", "/"} {
		for _, r := range got {
			if string(r) == bad {
				t.Fatalf("unsafe character %q in %q", bad, got)
			}
		}
	}
	if Account("u", "host.example.com", 22) != "u@host.example.com:22" {
		t.Fatal("plain accounts must be unchanged")
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	for _, s := range []string{"", "p@ss w0rd", "pässwörd ✓", "line\nbreak"} {
		if got := decode(encode(s) + "\n"); got != s {
			t.Errorf("round trip %q → %q", s, got)
		}
	}
	if decode("legacy-plain") != "legacy-plain" {
		t.Error("unprefixed values pass through")
	}
}
