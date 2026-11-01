package configloader

import (
	"errors"
	"flag"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testDB struct {
	URL      string `config:",required"`
	Password string `config:",secret"`
	Pool     int    `default:"4"`
}

type testConfig struct {
	ListenURL string `default:":8080"`
	Debug     bool
	Timeout   time.Duration `default:"30s"`
	Themes    []string
	Secret    []byte `config:",secret"`
	IP        net.IP
	Skipped   string `config:"-"`
	Database  testDB
	unexp     string
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsAndSourcesOrder(t *testing.T) {
	file := writeFile(t, `{"ListenURL": ":1", "Timeout": "2m", "Themes": ["a", "b"], "Database": {"URL": "from-file", "Pool": 9}}`)
	t.Setenv("APP_LISTEN_URL", ":2")
	t.Setenv("APP_DATABASE_PASSWORD", "pw")
	var c testConfig
	err := Load(&c, File(file), Env("APP"), Flags([]string{"-listen-url", ":3", "-debug", "-ip", "10.0.0.1", "-themes", "x, y"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenURL != ":3" { // flag beats env beats file
		t.Errorf("ListenURL = %q", c.ListenURL)
	}
	if c.Timeout != 2*time.Minute || !c.Debug || c.Database.URL != "from-file" || c.Database.Pool != 9 || c.Database.Password != "pw" {
		t.Errorf("unexpected %+v", c)
	}
	if strings.Join(c.Themes, "|") != "x|y" || !c.IP.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Errorf("themes/ip: %v %v", c.Themes, c.IP)
	}
}

func TestDefaultsOnly(t *testing.T) {
	var c testConfig
	t.Setenv("APP_DATABASE_URL", "x")
	if err := Load(&c, Env("APP")); err != nil {
		t.Fatal(err)
	}
	if c.ListenURL != ":8080" || c.Timeout != 30*time.Second || c.Database.Pool != 4 {
		t.Errorf("defaults: %+v", c)
	}
}

func TestAllErrorsAtOnceAndHints(t *testing.T) {
	t.Setenv("APP_TIMEOUT", "soon")
	t.Setenv("APP_DATABASE_PASSWORD", "x")
	t.Setenv("APP_DATABASE_POOL", "many")
	var c testConfig
	err := Load(&c, Env("APP"), Flags(nil))
	if err == nil {
		t.Fatal("want error")
	}
	msg := err.Error()
	for _, want := range []string{
		"Timeout (env APP_TIMEOUT)",
		"Database.Pool (env APP_DATABASE_POOL)",
		"Database.URL: required, set it with APP_DATABASE_URL, -database-url",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Error("errors.As cannot find a FieldError")
	}
}

func TestSecretNeverInErrors(t *testing.T) {
	type c struct {
		Port int `config:",secret"`
	}
	t.Setenv("PORT", "hunter2")
	var v c
	err := Load(&v, Env(""))
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("secret leaked or no error: %v", err)
	}
	if got := Dump(&testConfig{Secret: []byte("abc")}); strings.Contains(got, "abc") || !strings.Contains(got, "Secret = ***") {
		t.Errorf("Dump: %s", got)
	}
}

func TestFileProblems(t *testing.T) {
	var c testConfig
	if err := Load(&c, File(filepath.Join(t.TempDir(), "nope.json"))); err == nil {
		t.Error("a missing File must fail")
	}
	t.Setenv("DATABASE_URL", "x")
	if err := Load(&c, OptionalFile(filepath.Join(t.TempDir(), "nope.json")), Env("")); err != nil {
		t.Errorf("OptionalFile: %v", err)
	}
	err := Load(&c, File(writeFile(t, `{"Databse": {"URL": "x"}, "Debug": "maybe"}`)))
	if err == nil || !strings.Contains(err.Error(), `unknown key "databse.url"`) || !strings.Contains(err.Error(), "Debug (file") {
		t.Errorf("typo and bad value: %v", err)
	}
	if err := Load(&c, File(writeFile(t, `{not json`))); err == nil {
		t.Error("bad JSON must fail")
	}
}

func TestFlagsDoNotTouchGlobals(t *testing.T) {
	var c testConfig
	err := Load(&c, Flags([]string{"-nope"}))
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown flag: %v", err)
	}
	if err := Load(&c, Flags([]string{"-h"})); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("-h: %v", err)
	}
}

type validated struct {
	Min, Max int
}

func (v validated) Validate() error {
	if v.Min > v.Max {
		return errors.New("Min is greater than Max")
	}
	return nil
}

func TestValidate(t *testing.T) {
	v := validated{Min: 5}
	if err := Load(&v); err == nil || !strings.Contains(err.Error(), "Min is greater") {
		t.Errorf("Validate not used: %v", err)
	}
}

func TestBadTargets(t *testing.T) {
	if Load(testConfig{}) == nil || Load(nil) == nil || Load(new(int)) == nil {
		t.Error("non-pointer targets must fail")
	}
	if err := Load(&struct{ M map[string]string }{}); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("unsupported type: %v", err)
	}
	if err := Load(&struct {
		A string `config:"x"`
		B string `config:"x"`
	}{}); err == nil || !strings.Contains(err.Error(), "same name") {
		t.Errorf("duplicate names: %v", err)
	}
	if err := Load(&struct {
		A string `config:",requird"`
	}{}); err == nil {
		t.Error("unknown option must fail")
	}
}

func TestSnake(t *testing.T) {
	for in, want := range map[string]string{
		"ListenURL": "listen_url", "DatabaseURL": "database_url", "URL": "url",
		"Pool": "pool", "HTTPServer": "http_server", "UIScale": "ui_scale", "V2Name": "v2_name",
	} {
		if got := snake(in); got != want {
			t.Errorf("snake(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEmbeddedAndPointerKinds(t *testing.T) {
	type base struct{ Name string }
	type c struct {
		base
		Ratio float32
		Small int8
	}
	t.Setenv("NAME", "n")
	t.Setenv("RATIO", "1.5")
	t.Setenv("SMALL", "300")
	var v c
	err := Load(&v, Env(""))
	if err == nil || !strings.Contains(err.Error(), "Small") || v.Name != "n" || v.Ratio != 1.5 {
		t.Errorf("embedded/overflow: %v %+v", err, v)
	}
}
