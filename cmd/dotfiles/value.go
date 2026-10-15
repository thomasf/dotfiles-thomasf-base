package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"math/big"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"go.starlark.net/starlark"
)

// The structured file formats of [[starlark]] and read().
const (
	formatJSON = "json"
	formatTOML = "toml"
)

// formatOf returns the format of path from its extension.
func formatOf(path string) (string, error) {
	switch filepath.Ext(path) {
	case ".json":
		return formatJSON, nil
	case ".toml":
		return formatTOML, nil
	}
	return "", fmt.Errorf("unknown format of %s: want .json or .toml", path)
}

// decodeDocument decodes data in format into a dict. Data that is empty or
// only white space is an empty dict; a top level that is not a map is an
// error.
func decodeDocument(format string, data []byte) (*starlark.Dict, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return starlark.NewDict(0), nil
	}
	var v any
	switch format {
	case formatJSON:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			return nil, errors.New("unexpected data after the top-level value")
		}
	case formatTOML:
		if err := toml.Unmarshal(data, &v); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown format %q", format)
	}
	sv, err := fromGo(v, "")
	if err != nil {
		return nil, err
	}
	d, ok := sv.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("top level is a %s, not a map", sv.Type())
	}
	return d, nil
}

// encodeDocument encodes v in format, with keys sorted. JSON is indented by
// indent spaces.
func encodeDocument(format string, v *starlark.Dict, indent int) ([]byte, error) {
	gv, err := toGo(v, format, "")
	if err != nil {
		return nil, err
	}
	switch format {
	case formatJSON:
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", strings.Repeat(" ", indent))
		if err := enc.Encode(gv); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	case formatTOML:
		return toml.Marshal(gv)
	}
	return nil, fmt.Errorf("unknown format %q", format)
}

// keyPath returns the path of key in the value at path, for error messages.
func keyPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// at prefixes err with path, unless path or err is empty.
func at(path string, err error) error {
	if path == "" || err == nil {
		return err
	}
	return fmt.Errorf("%s: %w", path, err)
}

// valuesEqual reports whether a and b are equal as decoded documents: dicts
// regardless of key order, lists and tuples element by element, and scalars
// only if they also have the same type, so that 1 and 1.0 differ. NaN equals
// NaN, so that a file containing one is not rewritten on every run.
func valuesEqual(a, b starlark.Value) bool {
	switch a := a.(type) {
	case *starlark.Dict:
		b, ok := b.(*starlark.Dict)
		if !ok || a.Len() != b.Len() {
			return false
		}
		for _, item := range a.Items() {
			bv, found, err := b.Get(item[0])
			if err != nil || !found || !valuesEqual(item[1], bv) {
				return false
			}
		}
		return true
	case *starlark.List, starlark.Tuple:
		as := a.(starlark.Indexable)
		bs, ok := b.(starlark.Indexable)
		if _, isStr := b.(starlark.String); !ok || isStr || as.Len() != bs.Len() {
			return false
		}
		for i := range as.Len() {
			if !valuesEqual(as.Index(i), bs.Index(i)) {
				return false
			}
		}
		return true
	case starlark.Float:
		b, ok := b.(starlark.Float)
		return ok && (a == b || math.IsNaN(float64(a)) && math.IsNaN(float64(b)))
	}
	if a.Type() != b.Type() {
		return false
	}
	eq, err := starlark.Equal(a, b)
	return err == nil && eq
}

// fromGo converts a value decoded by encoding/json or go-toml, or built by
// exprVars, to a Starlark value. A JSON number without a fraction or
// exponent is an int, any other a float.
func fromGo(v any, path string) (starlark.Value, error) {
	switch v := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(v), nil
	case string:
		return starlark.String(v), nil
	case int64:
		return starlark.MakeInt64(v), nil
	case float64:
		return starlark.Float(v), nil
	case json.Number:
		if strings.ContainsAny(string(v), ".eE") {
			f, err := v.Float64()
			return starlark.Float(f), at(path, err)
		}
		i, ok := new(big.Int).SetString(string(v), 10)
		if !ok {
			return nil, at(path, fmt.Errorf("invalid integer %s", v))
		}
		return starlark.MakeBigInt(i), nil
	case []any:
		elems := make([]starlark.Value, len(v))
		for i, e := range v {
			sv, err := fromGo(e, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			elems[i] = sv
		}
		return starlark.NewList(elems), nil
	case map[string]any:
		d := starlark.NewDict(len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			sv, err := fromGo(v[k], keyPath(path, k))
			if err != nil {
				return nil, err
			}
			d.SetKey(starlark.String(k), sv)
		}
		return d, nil
	}
	return nil, at(path, fmt.Errorf("unsupported value %v of type %T (TOML dates and times are not supported)", v, v))
}

// toGo converts a Starlark value to the Go values that encoding/json or
// go-toml encode in format. JSON numbers are written as json.Number, so
// that big ints are exact and floats keep a fraction.
func toGo(v starlark.Value, format, path string) (any, error) {
	switch v := v.(type) {
	case starlark.NoneType:
		if format == formatTOML {
			return nil, at(path, errors.New("TOML has no null; delete the key instead"))
		}
		return nil, nil
	case starlark.Bool:
		return bool(v), nil
	case starlark.String:
		return string(v), nil
	case starlark.Int:
		if format == formatJSON {
			return json.Number(v.String()), nil
		}
		i, ok := v.Int64()
		if !ok {
			return nil, at(path, fmt.Errorf("integer %s does not fit in 64 bits", v))
		}
		return i, nil
	case starlark.Float:
		f := float64(v)
		if format == formatJSON {
			if math.IsInf(f, 0) || math.IsNaN(f) {
				return nil, at(path, fmt.Errorf("JSON cannot represent %v", f))
			}
			return json.Number(formatFloat(f)), nil
		}
		return f, nil
	case *starlark.Dict:
		m := make(map[string]any, v.Len())
		for _, item := range v.Items() {
			k, ok := item[0].(starlark.String)
			if !ok {
				return nil, at(path, fmt.Errorf("key %s is a %s, not a string", item[0], item[0].Type()))
			}
			gv, err := toGo(item[1], format, keyPath(path, string(k)))
			if err != nil {
				return nil, err
			}
			m[string(k)] = gv
		}
		return m, nil
	case *starlark.List, starlark.Tuple:
		s := v.(starlark.Indexable)
		out := make([]any, s.Len())
		for i := range s.Len() {
			gv, err := toGo(s.Index(i), format, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out[i] = gv
		}
		return out, nil
	}
	return nil, at(path, fmt.Errorf("cannot write a %s", v.Type()))
}

// formatFloat formats a finite f like encoding/json, but always with a
// fraction or an exponent, so that it reads back as a float.
func formatFloat(f float64) string {
	format := byte('f')
	if abs := math.Abs(f); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	s := strconv.FormatFloat(f, format, -1, 64)
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	return s
}
