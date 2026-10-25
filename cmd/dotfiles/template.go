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
func renderTemplate(name, text string, ec evalContext) ([]byte, error) {
	t, err := parseTemplate(name, text, ec)
	if err != nil {
		return nil, err
	}
	return executeTemplate(t, ec)
}

// parseTemplate parses text as a Go text/template, with the funcs from
// templateFuncs.
func parseTemplate(name, text string, ec evalContext) (*template.Template, error) {
	return template.New(name).Option("missingkey=error").Funcs(templateFuncs(ec)).Parse(text)
}

// templateFuncs returns the funcs of templates: those from exprFuncs plus
// expr, which evaluates an expr expression like a condition, with the content
// ec.vars has when it is called, and shellQuote.
func templateFuncs(ec evalContext) template.FuncMap {
	funcs := exprFuncs(ec.dstRoot)
	funcs["expr"] = func(src string) (any, error) {
		return evalExpr(exprEnv(ec), src)
	}
	funcs["shellQuote"] = shellQuote
	return funcs
}

// templateBuiltins are the functions that text/template predefines. The
// package does not export them.
var templateBuiltins = []string{
	"and", "call", "eq", "ge", "gt", "html", "index", "js", "le", "len", "lt",
	"ne", "not", "or", "print", "printf", "println", "slice", "urlquery",
}

// executeTemplate executes t, parsed by parseTemplate with the same ec.
func executeTemplate(t *template.Template, ec evalContext) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, exprVars(ec)); err != nil {
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
func renderValue(name, value string, ec evalContext) (string, error) {
	if !strings.Contains(value, "{{") {
		return value, nil
	}
	out, err := renderTemplate(name, value, ec)
	return string(out), err
}
