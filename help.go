package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// colorFor decides whether output written to f should be colored.
// Colors are on for terminals, off for pipes, off when NO_COLOR is set,
// and --color / --no-color on the command line override both.
func colorFor(f *os.File, args []string) palette {
	on := false
	if fi, err := f.Stat(); err == nil {
		on = fi.Mode()&os.ModeCharDevice != 0
	}
	if _, set := os.LookupEnv("NO_COLOR"); set {
		on = false
	}
	for _, a := range args {
		switch a {
		case "--color":
			on = true
		case "--no-color":
			on = false
		}
	}
	return palette{on: on}
}

type row struct{ left, desc string }

type group struct {
	title string
	rows  []row
}

var (
	urlRows = []row{
		{":3000/api", "→ http://localhost:3000/api"},
		{"localhost:3000/api", "→ http://localhost:3000/api"},
		{"example.com/path", "→ https://example.com/path"},
	}

	itemRows = []row{
		{"Name:Value", "Request header"},
		{"key==value", "Query string parameter"},
		{"key=value", "Body field, sent as a string"},
		{"key:=<json>", "Body field with a raw JSON value (numbers, booleans, arrays, objects)"},
		{"key@<path>", "File upload (switches to multipart/form-data)"},
	}

	pathRows = []row{
		{"user.name=John", `Nested object: {"user":{"name":"John"}}`},
		{"tags[]=a tags[]=b", `Append to an array: {"tags":["a","b"]}`},
		{"items[0].id:=1", `Index into an array of objects`},
		{`a\.b=1`, `Escape a literal dot or bracket in a key`},
	}

	flagGroups = []group{
		{"body", []row{
			{"-f, --form", "Send fields as application/x-www-form-urlencoded instead of JSON"},
			{"-d, --data <body>", "Send a raw request body (use @file to read it from a file)"},
		}},
		{"headers & auth", []row{
			{"-H, --header <Name:Value>", "Add a request header (same as a Name:Value item)"},
			{"-u, --user <user:pass>", "HTTP basic authentication"},
			{"    --bearer <token>", "Send an Authorization: Bearer header"},
		}},
		{"output", []row{
			{"-v, --verbose", "Also print the request that was sent"},
			{"-b, --body", "Print only the response body"},
			{"-I, --headers-only", "Print only the status line and response headers"},
			{"-i, --include", "Show status and headers even when output is piped"},
			{"    --raw", "Do not pretty-print the body"},
			{"-o, --output <file>", "Save the response body to a file"},
			{"    --color, --no-color", "Force colors on or off"},
		}},
		{"network", []row{
			{"-t, --timeout <secs>", "Give up after this long (default 30; 500ms and 2m also work)"},
			{"    --no-follow", "Do not follow redirects"},
			{"-k, --insecure", "Skip TLS certificate verification"},
			{"    --fail", "Exit with status 1 on HTTP 4xx/5xx responses"},
		}},
		{"misc", []row{
			{"-h, --help", "Show this help"},
			{"    --version", "Print the version"},
		}},
	}

	exampleRows = []row{
		{"Fetch JSON from a local server", "req :3000/users"},
		{"Create a user with a nested JSON body", "req POST :3000/users user.name=John user.address.city=Amsterdam roles[]=admin active:=true"},
		{"Update a resource with a bearer token", "req PUT :3000/users/1 name=John --bearer $TOKEN"},
		{"Submit a login form", "req POST :3000/login -f email=me@example.com password=secret"},
		{"Upload a file along with a field", "req POST :3000/upload title=Cat photo@./cat.png"},
		{"Add query string parameters", "req api.example.com/search q==golang page==2"},
		{"See the request and the response", "req -v :3000/health"},
		{"Pipe the body into jq", "req api.example.com/items | jq '.[0]'"},
	}
)

var reAngle = regexp.MustCompile(`<[^>]*>`)

// colorSyntax colors s with code, dimming <placeholders>.
func colorSyntax(p palette, s, code string) string {
	var b strings.Builder
	pos := 0
	for _, m := range reAngle.FindAllStringIndex(s, -1) {
		b.WriteString(p.wrap(code, s[pos:m[0]]))
		b.WriteString(p.dim(s[m[0]:m[1]]))
		pos = m[1]
	}
	b.WriteString(p.wrap(code, s[pos:]))
	return b.String()
}

func printRows(w io.Writer, p palette, rows []row, code string) {
	width := 0
	for _, r := range rows {
		if n := len([]rune(r.left)); n > width {
			width = n
		}
	}
	for _, r := range rows {
		pad := strings.Repeat(" ", width-len([]rune(r.left)))
		fmt.Fprintf(w, "    %s%s  %s\n", colorSyntax(p, r.left, code), pad, r.desc)
	}
}

func usageLine(p palette) string {
	return strings.Join([]string{
		p.bold("req"), p.dim("[METHOD]"), p.bold("URL"), p.dim("[ITEM...]"), p.dim("[FLAGS]"),
	}, " ")
}

func printHelp(w io.Writer, p palette) {
	fmt.Fprintf(w, "%s %s  %s\n", p.wrap("1;36", "req"), p.dim(version), "a tiny HTTP client for the terminal")
	fmt.Fprintf(w, "%s\n\n", p.dim("Test websites and APIs with readable, colorful output."))

	fmt.Fprintln(w, p.bold("usage:"))
	fmt.Fprintf(w, "    %s\n\n", usageLine(p))

	fmt.Fprintln(w, p.bold("method:"))
	fmt.Fprintf(w, "    GET POST PUT PATCH DELETE HEAD OPTIONS\n")
	fmt.Fprintf(w, "    %s\n\n", p.dim("Defaults to GET, or POST when the request has a body."))

	fmt.Fprintln(w, p.bold("url:"))
	printRows(w, p, urlRows, "36")
	fmt.Fprintln(w)

	fmt.Fprintln(w, p.bold("items:"))
	printRows(w, p, itemRows, "33")
	fmt.Fprintln(w)

	fmt.Fprintln(w, p.bold("nested json keys:"))
	printRows(w, p, pathRows, "33")
	fmt.Fprintf(w, "    %s\n\n", p.dim("Nesting applies to JSON bodies; form and multipart fields stay flat."))

	fmt.Fprintln(w, p.bold("flags:"))
	for i, g := range flagGroups {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "  %s\n", p.dim(g.title))
		printRows(w, p, g.rows, "32")
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, p.bold("examples:"))
	for i, e := range exampleRows {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "    %s\n", p.dim("# "+e.left))
		fmt.Fprintf(w, "    %s %s%s\n", p.dim("$"), p.bold("req"), strings.TrimPrefix(e.desc, "req"))
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, p.bold("more:"))
	fmt.Fprintf(w, "    docs, issues and source: %s\n", p.wrap("4", repoURL))
}

// printUsageError prints a clap/HTTPie-style usage error.
func printUsageError(w io.Writer, p palette, msg string) {
	fmt.Fprintln(w, p.bold("usage:"))
	fmt.Fprintf(w, "    %s\n\n", usageLine(p))
	fmt.Fprintln(w, p.wrap("1;31", "error:"))
	fmt.Fprintf(w, "    %s\n\n", msg)
	fmt.Fprintln(w, p.bold("for more information:"))
	fmt.Fprintf(w, "    run '%s' or visit %s\n", p.green("req --help"), p.wrap("4", repoURL))
}

func errorHint(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Timeout() {
		return "the request timed out; raise the limit with -t <seconds>"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "nothing is listening there - is the server running, and is the port right?"
	case strings.Contains(msg, "no such host"):
		return "could not resolve the hostname - check for typos and your network"
	case strings.Contains(msg, "certificate"):
		return "TLS verification failed - use -k to skip it (only for servers you trust)"
	}
	return ""
}

// printRuntimeError prints errors that happen while building or sending a request.
func printRuntimeError(w io.Writer, p palette, err error) {
	msg := err.Error()
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		msg = ue.Err.Error()
	}
	fmt.Fprintf(w, "%s %s\n", p.wrap("1;31", "error:"), msg)
	if h := errorHint(err); h != "" {
		fmt.Fprintf(w, "%s  %s\n", p.wrap("1;33", "hint:"), h)
	}
}
