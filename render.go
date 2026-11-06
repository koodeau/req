package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ---------- palette ----------

type palette struct{ on bool }

func (p palette) wrap(code, s string) string {
	if !p.on || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p palette) dim(s string) string     { return p.wrap("2", s) }
func (p palette) bold(s string) string    { return p.wrap("1", s) }
func (p palette) cyan(s string) string    { return p.wrap("36", s) }
func (p palette) green(s string) string   { return p.wrap("32", s) }
func (p palette) yellow(s string) string  { return p.wrap("33", s) }
func (p palette) magenta(s string) string { return p.wrap("35", s) }

func (p palette) status(code int, s string) string {
	switch {
	case code >= 500:
		return p.wrap("1;35", s)
	case code >= 400:
		return p.wrap("1;31", s)
	case code >= 300:
		return p.wrap("1;33", s)
	case code >= 200:
		return p.wrap("1;32", s)
	}
	return p.wrap("1", s)
}

// newPalette decides whether to colorize and whether stdout is a terminal.
func newPalette(c *config) (palette, bool) {
	tty := false
	if fi, err := os.Stdout.Stat(); err == nil {
		tty = fi.Mode()&os.ModeCharDevice != 0
	}
	on := tty
	if _, set := os.LookupEnv("NO_COLOR"); set {
		on = false
	}
	if c.forceColor {
		on = true
	}
	if c.noColor {
		on = false
	}
	return palette{on: on}, tty
}

// ---------- helpers ----------

func humanBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

func quote(s string) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	return strings.TrimRight(b.String(), "\n")
}

func indent(depth int) string { return strings.Repeat("  ", depth) }

// ---------- JSON ----------

// json pretty-prints raw JSON, preserving key order. ok=false if it isn't valid JSON.
func (p palette) json(raw []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return "", false
	}
	var sb strings.Builder
	if err := p.jsonValue(dec, &sb, tok, 0); err != nil {
		return "", false
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", false
	}
	return sb.String(), true
}

func (p palette) jsonValue(dec *json.Decoder, sb *strings.Builder, tok json.Token, depth int) error {
	switch t := tok.(type) {
	case json.Delim:
		closer := "}"
		if t == '[' {
			closer = "]"
		}
		sb.WriteString(string(t))
		if !dec.More() {
			if _, err := dec.Token(); err != nil {
				return err
			}
			sb.WriteString(closer)
			return nil
		}
		sb.WriteString("\n")
		for dec.More() {
			sb.WriteString(indent(depth + 1))
			if t == '{' {
				k, err := dec.Token()
				if err != nil {
					return err
				}
				ks, _ := k.(string)
				sb.WriteString(p.wrap("1;34", quote(ks)))
				sb.WriteString(": ")
			}
			v, err := dec.Token()
			if err != nil {
				return err
			}
			if err := p.jsonValue(dec, sb, v, depth+1); err != nil {
				return err
			}
			if dec.More() {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		if _, err := dec.Token(); err != nil {
			return err
		}
		sb.WriteString(indent(depth))
		sb.WriteString(closer)
	case string:
		sb.WriteString(p.green(quote(t)))
	case json.Number:
		sb.WriteString(p.cyan(t.String()))
	case bool:
		sb.WriteString(p.yellow(strconv.FormatBool(t)))
	case nil:
		sb.WriteString(p.magenta("null"))
	}
	return nil
}

// ---------- HTML / XML (colorize only, no re-indenting yet) ----------

var (
	reMarkup = regexp.MustCompile(`<!--[\s\S]*?-->|<(/?)([A-Za-z!?][\w:.-]*)((?:[^>"']|"[^"]*"|'[^']*')*)>`)
	reAttr   = regexp.MustCompile(`([\w:.-]+)=("[^"]*"|'[^']*')`)
)

func (p palette) markup(s string) string {
	if !p.on {
		return s
	}
	return reMarkup.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, "<!--") {
			return p.dim(m)
		}
		sm := reMarkup.FindStringSubmatch(m)
		attrs := reAttr.ReplaceAllStringFunc(sm[3], func(a string) string {
			am := reAttr.FindStringSubmatch(a)
			return p.yellow(am[1]) + "=" + p.green(am[2])
		})
		return p.dim("<"+sm[1]) + p.wrap("1;34", sm[2]) + attrs + p.dim(">")
	})
}

// ---------- body / headers ----------

func formatBody(p palette, c *config, ct string, body []byte) string {
	if !utf8.Valid(body) {
		return p.dim(fmt.Sprintf("[binary data, %s - use -o <file> to save]", humanBytes(len(body))))
	}
	s := string(body)
	if c.raw {
		return strings.TrimRight(s, "\n")
	}
	trim := strings.TrimSpace(s)
	ct = strings.ToLower(ct)
	switch {
	case strings.Contains(ct, "json") || strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "["):
		if out, ok := p.json([]byte(trim)); ok {
			return out
		}
	case strings.Contains(ct, "xml") || strings.Contains(ct, "html") || strings.HasPrefix(trim, "<"):
		return p.markup(trim)
	}
	return trim
}

func printHeaders(p palette, h http.Header) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range h[k] {
			fmt.Printf("%s: %s\n", p.cyan(k), v)
		}
	}
}

func printRequest(p palette, req *http.Request, body []byte) {
	fmt.Printf("%s %s %s\n", p.wrap("1;36", req.Method), req.URL.RequestURI(), p.dim("HTTP/1.1"))
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	fmt.Printf("%s: %s\n", p.cyan("Host"), host)
	printHeaders(p, req.Header)
	if len(body) > 0 {
		fmt.Println()
		ct := req.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "multipart/") {
			fmt.Println(p.dim(fmt.Sprintf("[multipart body, %s]", humanBytes(len(body)))))
		} else {
			fmt.Println(formatBody(p, &config{}, ct, body))
		}
	}
	fmt.Println()
}

func printResponse(p palette, c *config, tty bool, resp *http.Response, body []byte, elapsed time.Duration) {
	showMeta := !c.bodyOnly && (tty || c.include || c.headersOnly || c.verbose)
	if showMeta {
		meta := fmt.Sprintf("· %s · %s", elapsed.Round(time.Millisecond), humanBytes(len(body)))
		fmt.Printf("%s %s  %s\n", p.dim(resp.Proto), p.status(resp.StatusCode, resp.Status), p.dim(meta))
		printHeaders(p, resp.Header)
		if !c.headersOnly && len(body) > 0 {
			fmt.Println()
		}
	}
	if c.headersOnly || len(body) == 0 {
		return
	}
	fmt.Println(formatBody(p, c, resp.Header.Get("Content-Type"), body))
}
