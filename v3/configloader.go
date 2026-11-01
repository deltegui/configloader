// Package configloader fills a configuration struct from a file, environment
// variables and command line flags, and tells you everything that is wrong
// with it instead of stopping at the first problem.
//
// It never exits and never logs: Load returns an error and the program decides
// what to do. A program that wants to refuse to start with a bad configuration
// does this:
//
//	var conf Config
//	if err := configloader.Load(&conf,
//		configloader.OptionalFile("config.json"),
//		configloader.Env("APP"),
//		configloader.Flags(os.Args[1:]),
//	); err != nil {
//		log.Fatalf("invalid configuration:\n%v", err)
//	}
//
// Sources are applied in order and the later ones win. Before them the
// defaults are applied (the value the struct already had, or its `default` tag).
//
// # Struct tags
//
//	config:"name,required,secret"  (all parts optional; "-" skips the field)
//	default:"8080"                 value when no source sets the field
//	usage:"what it is"             description shown by -h
//
// The name replaces the Go name of the field in every source. A field is
// required if no source (and no default) left it with something other than its
// zero value. A secret is never written in an error or by Dump.
//
// Nested structs add their name as a prefix: Database.URL is DATABASE_URL in the
// environment (with Env("APP"), APP_DATABASE_URL), -database-url as a flag and
// {"Database": {"URL": ...}} in the file. Embedded structs add no prefix.
//
// # Types
//
// string, bool, all ints, uints and floats, time.Duration ("30s"), anything that
// implements encoding.TextUnmarshaler (net.IP, time.Time, slog.Level...) and
// slices of those. In the environment and flags a slice is a comma separated
// list; in the file it is a JSON array. A []byte is the text itself.
//
// If the target has a method Validate() error it is called at the end, and what
// it returns is part of the error, for checks that involve several fields.
package configloader

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
)

// FieldError is a problem with one field. Load joins them (errors.Join), so
// errors.As finds them one by one.
type FieldError struct {
	Field  string // Go path: Database.URL
	Source string // where the value came from: "env APP_X", "flag -x"... or empty
	Err    error
}

func (e *FieldError) Error() string {
	if e.Source == "" {
		return e.Field + ": " + e.Err.Error()
	}
	return e.Field + " (" + e.Source + "): " + e.Err.Error()
}

func (e *FieldError) Unwrap() error { return e.Err }

// Source is where values come from: File, OptionalFile, Env and Flags.
type Source interface {
	apply(fields []*field) []error
	// hint tells how this source would set the field, for the "required" message.
	hint(f *field) string
}

// Load fills target, a pointer to a struct, from the sources in order.
func Load(target any, sources ...Source) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return errors.New("configloader: target must be a non-nil pointer to a struct")
	}
	fields, err := collect(rv.Elem(), nil, "")
	if err != nil {
		return err
	}
	if err := checkNames(fields); err != nil {
		return err
	}

	var errs []error
	for _, f := range fields {
		if !f.hasDefault {
			continue
		}
		if err := setString(f.v, f.def); err != nil {
			errs = append(errs, f.fail("default", err))
		}
	}
	for _, s := range sources {
		errs = append(errs, s.apply(fields)...)
	}
	for _, f := range fields {
		if f.required && f.v.IsZero() {
			errs = append(errs, &FieldError{Field: f.goPath, Err: errors.New("required, set it with " + hints(f, sources))})
		}
	}
	if v, ok := target.(interface{ Validate() error }); ok && len(errs) == 0 {
		if err := v.Validate(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func hints(f *field, sources []Source) string {
	var parts []string
	for _, s := range sources {
		if h := s.hint(f); h != "" {
			parts = append(parts, h)
		}
	}
	if len(parts) == 0 {
		return "a default in the struct"
	}
	return strings.Join(parts, ", ")
}

// Dump is the effective value of every field, one per line, with secrets hidden.
// It is what you log at startup to know what the program is running with.
func Dump(target any) string {
	rv := reflect.ValueOf(target)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	fields, err := collect(rv, nil, "")
	if err != nil {
		return err.Error()
	}
	var b strings.Builder
	for _, f := range fields {
		val := fmt.Sprint(f.v.Interface())
		if f.secret && !f.v.IsZero() {
			val = "***"
		}
		fmt.Fprintf(&b, "%s = %s\n", f.goPath, val)
	}
	return b.String()
}

type field struct {
	v          reflect.Value
	path       []string // names as the sources see them
	goPath     string
	required   bool
	secret     bool
	def        string
	hasDefault bool
	usage      string
}

func (f *field) fail(source string, err error) *FieldError {
	if f.secret {
		err = errors.New("invalid value (hidden)")
	}
	return &FieldError{Field: f.goPath, Source: source, Err: err}
}

// key is the lower case dotted path, how a file key is matched.
func (f *field) key() string { return strings.ToLower(strings.Join(f.path, ".")) }

// words is the path in lower snake case segments joined by underscore.
func (f *field) words() []string {
	var out []string
	for _, p := range f.path {
		out = append(out, snake(p))
	}
	return out
}

func (f *field) envName(prefix string) string {
	name := strings.ToUpper(strings.Join(f.words(), "_"))
	if prefix == "" {
		return name
	}
	return strings.ToUpper(prefix) + "_" + name
}

func (f *field) flagName() string { return strings.ReplaceAll(strings.Join(f.words(), "-"), "_", "-") }

func collect(v reflect.Value, path []string, goPrefix string) ([]*field, error) {
	var out []*field
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		// An embedded struct with a lower case type still has exported fields.
		if !sf.IsExported() && !(sf.Anonymous && isStruct(sf.Type)) {
			continue
		}
		name, opts, _ := strings.Cut(sf.Tag.Get("config"), ",")
		if name == "-" {
			continue
		}
		goPath := goPrefix + sf.Name
		fv := v.Field(i)

		if isStruct(sf.Type) {
			sub := path
			if !(sf.Anonymous && name == "") {
				sub = append(append([]string{}, path...), orName(name, sf.Name))
			}
			prefix := goPath + "."
			if sf.Anonymous && name == "" {
				prefix = goPrefix
			}
			inner, err := collect(fv, sub, prefix)
			if err != nil {
				return nil, err
			}
			out = append(out, inner...)
			continue
		}

		if err := checkType(sf.Type); err != nil {
			return nil, fmt.Errorf("configloader: field %s: %w", goPath, err)
		}
		f := &field{v: fv, path: append(append([]string{}, path...), orName(name, sf.Name)), goPath: goPath, usage: sf.Tag.Get("usage")}
		f.def, f.hasDefault = sf.Tag.Lookup("default")
		for _, o := range strings.Split(opts, ",") {
			switch strings.TrimSpace(o) {
			case "required":
				f.required = true
			case "secret":
				f.secret = true
			case "":
			default:
				return nil, fmt.Errorf("configloader: field %s: unknown option %q", goPath, o)
			}
		}
		out = append(out, f)
	}
	return out, nil
}

func orName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}

// isStruct is true for a struct that is a group of fields, and false for one
// that is a value (time.Time, anything with UnmarshalText).
func isStruct(t reflect.Type) bool {
	return t.Kind() == reflect.Struct && !reflect.PointerTo(t).Implements(textUnmarshaler)
}

// checkNames refuses two fields that a source could not tell apart.
func checkNames(fields []*field) error {
	seen := map[string]string{}
	for _, f := range fields {
		for _, k := range []string{f.key(), strings.Join(f.words(), "_")} {
			if other, ok := seen[k]; ok && other != f.goPath {
				return fmt.Errorf("configloader: fields %s and %s have the same name %q", other, f.goPath, k)
			}
			seen[k] = f.goPath
		}
	}
	return nil
}

// snake turns ListenURL into listen_url and DatabaseURL into database_url.
func snake(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if unicode.IsUpper(c) && i > 0 {
			prev := r[i-1]
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}
