package bridge

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestConfigurationFormats(t *testing.T) {
	c, err := ParseConfiguration([]byte(testProfile))
	if err != nil {
		t.Fatal(err)
	}
	binary, _ := proto.Marshal(c)
	values := url.Values{"profile": {"test"}, "port": {"4012-4021"}, "protocol": {"UDP"}, "multiplexing": {"MULTIPLEXING_HIGH"}, "handshake-mode": {"HANDSHAKE_NO_WAIT"}}
	u := url.URL{Scheme: "mierus", Host: "[2001:db8::1]", User: url.UserPassword("unit-user", "not-a-real-secret"), RawQuery: values.Encode()}
	for name, input := range map[string]string{
		"profile":       testProfile,
		"config":        `{"activeProfile":"test","profiles":[` + testProfile + `],"socks5Port":1080}`,
		"clash_json":    `{"name":"test","type":"mieru","server":"example.org","port-range":"4012-4021","transport":"UDP","username":"unit-user","password":"not-a-real-secret","multiplexing":"MULTIPLEXING_HIGH","handshake-mode":"HANDSHAKE_NO_WAIT"}`,
		"clash_yaml":    "proxies:\n  - name: direct\n    type: direct\n  - name: test\n    type: mieru\n    server: example.org\n    port: 443\n    username: unit-user\n    password: not-a-real-secret\n",
		"official_link": "mieru://" + base64.StdEncoding.EncodeToString(binary),
		"simple_link":   u.String(),
	} {
		t.Run(name, func(t *testing.T) {
			out, err := NormalizeConfiguration([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			normalized, err := ParseConfiguration(out)
			if err != nil || normalized.GetActiveProfile() != "test" || len(normalized.GetProfiles()) != 1 {
				t.Fatal("normalization lost active profile")
			}
			if normalized.Profiles[0].GetUser().GetPassword() != "not-a-real-secret" {
				t.Fatal("password changed")
			}
		})
	}
}

func TestConfigurationValidation(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "2001:db8::1", "localhost", "mita.example.org", "xn--bcher-kva.example"} {
		if !validServerHost(host) {
			t.Fatalf("rejected valid host %q", host)
		}
	}
	for _, host := range []string{"", "https://example.org", "a:443", "[2001:db8::1]", "999.1.2.3", "a..org", "-a.org", "a-.org", "a b", "a\n.org"} {
		if validServerHost(host) {
			t.Fatalf("accepted invalid host %q", host)
		}
	}
	for _, port := range []string{"1", "65535", "4012-4021"} {
		if _, err := portBinding(port, "TCP"); err != nil {
			t.Fatal(err)
		}
	}
	for _, port := range []string{"", "0", "65536", "443x", "2-1", "1-2-3", "-1", "1.5", "1-65536"} {
		if _, err := portBinding(port, "TCP"); err == nil {
			t.Fatalf("accepted invalid port %q", port)
		}
	}
	for name, input := range map[string]string{
		"structure": `[]`, "empty": `{}`, "broken_json": `{"profileName":`,
		"missing_user":     `{"profileName":"test"}`,
		"missing_password": strings.Replace(testProfile, `"password":"not-a-real-secret"`, `"password":""`, 1),
		"username_bytes":   strings.Replace(testProfile, "unit-user", strings.Repeat("ж", 33), 1),
		"enum":             strings.Replace(testProfile, "HANDSHAKE_STANDARD", "BAD_SECRET_SENTINEL", 1),
		"mtu":              strings.TrimSuffix(testProfile, "}") + `,"mtu":100}`,
		"active":           `{"profiles":[` + testProfile + `],"activeProfile":"unknown"}`,
		"duplicate":        `{"profiles":[` + testProfile + `,` + testProfile + `],"activeProfile":"test"}`,
		"partial":          `{"proxies":[{"name":"valid","type":"mieru","server":"localhost","port":443,"username":"u","password":"p"},{"type":"mieru"}]}`,
		"link_enum":        `mierus://user:pass@example.org?profile=test&port=443&protocol=TCP&handshake-mode=BAD_SECRET_SENTINEL`,
		"yaml_alias":       "name: &name test\ntype: mieru\nserver: *name\n",
		"yaml_documents":   "name: test\n---\nname: ignored\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := NormalizeConfiguration([]byte(input))
			if err == nil || len(out) != 0 {
				t.Fatal("invalid input produced a configuration")
			}
			if strings.Contains(err.Error(), "BAD_SECRET_SENTINEL") || strings.Contains(err.Error(), "not-a-real-secret") {
				t.Fatal("diagnostic leaked input")
			}
		})
	}
	if _, err := ParseConfiguration(make([]byte, MaxConfigSize+1)); err == nil {
		t.Fatal("accepted oversized config")
	}
}

func TestNormalizationPreservesImportedSettings(t *testing.T) {
	profile := strings.TrimSuffix(testProfile, "}") + `,"mtu":1400}`
	out, err := NormalizeConfiguration([]byte(profile))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if json.Unmarshal(out, &config) != nil {
		t.Fatal("invalid normalized JSON")
	}
	c, _ := ParseConfiguration(out)
	p := c.Profiles[0]
	if len(p.Servers[0].PortBindings) != 2 || p.GetMtu() != 1400 || p.GetMultiplexing().GetLevel().String() != "MULTIPLEXING_HIGH" {
		t.Fatal("settings were lost")
	}
}
