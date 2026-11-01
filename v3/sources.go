package configloader

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"reflect"
	"sort"
	"strings"
)

// File reads a JSON file. A file that does not exist is an error (see
// OptionalFile), and so is a key that no field has, which catches typos.
func File(path string) Source { return fileSource{path: path} }

// OptionalFile is File that does nothing if the file does not exist.
func OptionalFile(path string) Source { return fileSource{path: path, optional: true} }

type fileSource struct {
	path     string
	optional bool
}

func (s fileSource) hint(f *field) string {
	return fmt.Sprintf("%q in %s", strings.Join(f.path, "."), s.path)
}

func (s fileSource) apply(fields []*field) []error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) && s.optional {
		return nil
	}
	if err != nil {
		return []error{fmt.Errorf("config file: %w", err)}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return []error{fmt.Errorf("config file %s: %w", s.path, err)}
	}

	values := map[string]any{}
	flatten(values, "", root)
	source := "file " + s.path
	var errs []error
	for _, f := range fields {
		x, ok := values[f.key()]
		if !ok {
			continue
		}
		delete(values, f.key())
		if x == nil {
			continue
		}
		if err := setAny(f.v, x); err != nil {
			errs = append(errs, f.fail(source, err))
		}
	}
	unknown := make([]string, 0, len(values))
	for k := range values {
		unknown = append(unknown, k)
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		errs = append(errs, fmt.Errorf("config file %s: unknown key %q", s.path, k))
	}
	return errs
}

func flatten(out map[string]any, prefix string, m map[string]any) {
	for k, v := range m {
		key := strings.ToLower(prefix + k)
		if sub, ok := v.(map[string]any); ok {
			flatten(out, key+".", sub)
		} else {
			out[key] = v
		}
	}
}

// Env reads variables named PREFIX_NAME in upper case (APP_LISTEN_URL for the
// field ListenURL with Env("APP")). An empty prefix reads LISTEN_URL. An empty
// variable counts as not set.
func Env(prefix string) Source { return envSource{prefix: prefix, lookup: os.LookupEnv} }

type envSource struct {
	prefix string
	lookup func(string) (string, bool)
}

func (s envSource) hint(f *field) string { return f.envName(s.prefix) }

func (s envSource) apply(fields []*field) []error {
	var errs []error
	for _, f := range fields {
		name := f.envName(s.prefix)
		raw, ok := s.lookup(name)
		if !ok || raw == "" {
			continue
		}
		if err := setString(f.v, raw); err != nil {
			errs = append(errs, f.fail("env "+name, err))
		}
	}
	return errs
}

// Flags reads command line flags named in lower case with dashes
// (-listen-url, -database-url). Pass os.Args[1:]. It uses its own FlagSet, so it
// does not touch the flags of the program. With -h it writes the list of flags
// to stderr and the error is flag.ErrHelp.
func Flags(args []string) Source { return flagSource{args: args} }

type flagSource struct{ args []string }

func (s flagSource) hint(f *field) string { return "-" + f.flagName() }

type flagValue struct {
	text    string
	boolean bool
}

func (v *flagValue) String() string   { return v.text }
func (v *flagValue) IsBoolFlag() bool { return v.boolean }
func (v *flagValue) Set(s string) error {
	v.text = s
	return nil
}

func (s flagSource) apply(fields []*field) []error {
	set := flag.NewFlagSet("config", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	values := make(map[string]*flagValue, len(fields))
	for _, f := range fields {
		usage := f.usage
		if usage == "" {
			usage = f.goPath
		}
		if f.required {
			usage += " (required)"
		}
		v := &flagValue{boolean: f.v.Kind() == reflect.Bool}
		values[f.flagName()] = v
		set.Var(v, f.flagName(), usage)
	}
	if err := set.Parse(s.args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			set.SetOutput(os.Stderr)
			set.PrintDefaults()
		}
		return []error{err}
	}
	var errs []error
	byFlag := make(map[string]*field, len(fields))
	for _, f := range fields {
		byFlag[f.flagName()] = f
	}
	set.Visit(func(fl *flag.Flag) {
		f := byFlag[fl.Name]
		if err := setString(f.v, values[fl.Name].text); err != nil {
			errs = append(errs, f.fail("flag -"+fl.Name, err))
		}
	})
	return errs
}
