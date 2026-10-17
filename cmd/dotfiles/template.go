package main

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"text/template"
)

// renderTemplate executes text as a Go text/template (see parseTemplate). The
// data is the variables from exprVars.
func renderTemplate(name, text, dstRoot string, vars map[string]string) ([]byte, error) {
	t, err := parseTemplate(name, text, dstRoot, vars)
	if err != nil {
		return nil, err
	}
	return executeTemplate(t, vars)
}

// parseTemplate parses text as a Go text/template. The funcs are those from
// exprFuncs plus expr, which evaluates an expr expression like a condition,
// with the content vars has when it is called, and shellQuote.
func parseTemplate(name, text, dstRoot string, vars map[string]string) (*template.Template, error) {
	funcs := exprFuncs(dstRoot)
	funcs["expr"] = func(src string) (any, error) {
		return evalExpr(exprEnv(dstRoot, vars), src)
	}
	funcs["shellQuote"] = shellQuote
	return template.New(name).Option("missingkey=error").Funcs(funcs).Parse(text)
}

// executeTemplate executes t, parsed by parseTemplate with the same vars.
func executeTemplate(t *template.Template, vars map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, exprVars(vars)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// shellQuote quotes s as a single word for a POSIX shell, so that it is read
// as s whatever characters it contains, newlines included. A non-empty string
// of only letters, digits and @%+=:,./_- is returned as it is; anything else
// is put in single quotes. A NUL byte cannot be quoted.
func shellQuote(s string) (string, error) {
	if strings.ContainsRune(s, 0) {
		return "", errors.New("shellQuote: a shell word cannot contain a NUL byte")
	}
	if shellSafeRe.MatchString(s) {
		return s, nil
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", nil
}

var shellSafeRe = regexp.MustCompile(`^[A-Za-z0-9@%+=:,./_-]+$`)

// renderValue renders a config value, such as a src or dst path, with
// renderTemplate. A value without "{{" is returned as is.
func renderValue(name, value, dstRoot string, vars map[string]string) (string, error) {
	if !strings.Contains(value, "{{") {
		return value, nil
	}
	out, err := renderTemplate(name, value, dstRoot, vars)
	return string(out), err
}
