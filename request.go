package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// field is one body item: kind 'd' = string, 'j' = raw JSON, 'f' = file path.
type field struct {
	kind byte
	key  string
	val  string
}

type item struct {
	kind byte // 'h' header, 'q' query, 'd', 'j', 'f'
	key  string
	val  string
}

func parseItem(s string) (item, error) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '=':
			if i+1 < len(s) && s[i+1] == '=' {
				return item{'q', s[:i], s[i+2:]}, nil
			}
			return item{'d', s[:i], s[i+1:]}, nil
		case ':':
			if i+1 < len(s) && s[i+1] == '=' {
				return item{'j', s[:i], s[i+2:]}, nil
			}
			return item{'h', s[:i], strings.TrimSpace(s[i+1:])}, nil
		case '@':
			return item{'f', s[:i], s[i+1:]}, nil
		}
	}
	return item{}, fmt.Errorf("can't parse item %q (expected Header:Value, key=value, key:=json, key==query or key@file)", s)
}

func normalizeURL(raw string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, ":") {
		return "http://localhost" + raw
	}
	host := raw
	if i := strings.IndexAny(raw, "/?"); i >= 0 {
		host = raw[:i]
	}
	if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.") ||
		strings.HasPrefix(host, "0.0.0.0") || strings.HasPrefix(host, "[::1]") {
		return "http://" + raw
	}
	return "https://" + raw
}

func jsonBody(fields []field) ([]byte, error) {
	root := &jnode{obj: true}
	for _, f := range fields {
		segs, err := parsePath(f.key)
		if err != nil {
			return nil, err
		}
		var raw []byte
		if f.kind == 'j' {
			raw = bytes.TrimSpace([]byte(f.val))
			if !json.Valid(raw) {
				return nil, fmt.Errorf("invalid JSON for %q: %s", f.key, f.val)
			}
		} else {
			raw = []byte(quote(f.val))
		}
		if err := root.set(segs, raw); err != nil {
			return nil, fmt.Errorf("field %q: %w", f.key, err)
		}
	}
	var b bytes.Buffer
	root.write(&b)
	return b.Bytes(), nil
}

func multipartBody(fields []field) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range fields {
		if f.kind != 'f' {
			if err := w.WriteField(f.key, f.val); err != nil {
				return nil, "", err
			}
			continue
		}
		file, err := os.Open(f.val)
		if err != nil {
			return nil, "", err
		}
		part, err := w.CreateFormFile(f.key, filepath.Base(f.val))
		if err != nil {
			file.Close()
			return nil, "", err
		}
		_, err = io.Copy(part, file)
		file.Close()
		if err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

func stdinPiped() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice == 0
}

func looksLikeJSON(b []byte) bool {
	t := bytes.TrimSpace(b)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[') && json.Valid(t)
}

func buildRequest(c *config) (*http.Request, []byte, error) {
	headers := http.Header{}
	query := url.Values{}
	var fields []field
	hasFile := false

	for _, raw := range c.items {
		it, err := parseItem(raw)
		if err != nil {
			return nil, nil, err
		}
		switch it.kind {
		case 'h':
			headers.Set(it.key, it.val)
		case 'q':
			query.Add(it.key, it.val)
		default:
			if it.kind == 'f' {
				hasFile = true
			}
			fields = append(fields, field{it.kind, it.key, it.val})
		}
	}

	u, err := url.Parse(normalizeURL(c.url))
	if err != nil {
		return nil, nil, fmt.Errorf("bad url: %w", err)
	}
	if len(query) > 0 {
		q := u.Query()
		for k, vs := range query {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
	}

	var body []byte
	ctype := ""
	jsonMode := false
	switch {
	case c.hasRawData:
		body = []byte(c.rawData)
		if strings.HasPrefix(c.rawData, "@") {
			body, err = os.ReadFile(c.rawData[1:])
			if err != nil {
				return nil, nil, err
			}
		}
		if looksLikeJSON(body) {
			ctype = "application/json"
			jsonMode = true
		}
	case hasFile:
		body, ctype, err = multipartBody(fields)
	case len(fields) > 0 && c.form:
		vals := url.Values{}
		for _, f := range fields {
			vals.Add(f.key, f.val)
		}
		body = []byte(vals.Encode())
		ctype = "application/x-www-form-urlencoded"
	case len(fields) > 0:
		body, err = jsonBody(fields)
		ctype = "application/json"
		jsonMode = true
	case c.method != "" && c.method != "GET" && c.method != "HEAD" && c.method != "OPTIONS" && stdinPiped():
		body, err = io.ReadAll(os.Stdin)
		if err == nil && looksLikeJSON(body) {
			ctype = "application/json"
			jsonMode = true
		}
	}
	if err != nil {
		return nil, nil, err
	}

	method := c.method
	if method == "" {
		if len(body) > 0 {
			method = "POST"
		} else {
			method = "GET"
		}
	}

	req, err := http.NewRequest(method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("User-Agent", "req/"+version)
	if jsonMode {
		req.Header.Set("Accept", "application/json, */*;q=0.5")
	} else {
		req.Header.Set("Accept", "*/*")
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if c.user != "" {
		usr, pw, _ := strings.Cut(c.user, ":")
		req.SetBasicAuth(usr, pw)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	for k, vs := range headers {
		req.Header[k] = vs
	}
	if h := headers.Get("Host"); h != "" {
		req.Host = h
		req.Header.Del("Host")
	}
	return req, body, nil
}

func run(c *config) (int, error) {
	req, body, err := buildRequest(c)
	if err != nil {
		return 0, err
	}

	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if c.insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	client := &http.Client{Timeout: c.timeout, Transport: transport}
	if c.noFollow {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	p, tty := newPalette(c)
	if c.verbose {
		printRequest(p, req, body)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	elapsed := time.Since(start)

	if c.output != "" {
		if err := os.WriteFile(c.output, respBody, 0o644); err != nil {
			return 0, err
		}
		c.bodyOnly = false
		c.headersOnly = true
		printResponse(p, c, true, resp, respBody, elapsed)
		fmt.Fprintf(os.Stderr, "saved %s to %s\n", humanBytes(len(respBody)), c.output)
	} else {
		printResponse(p, c, tty, resp, respBody, elapsed)
	}

	if c.fail && resp.StatusCode >= 400 {
		return 1, nil
	}
	return 0, nil
}
