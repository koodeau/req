package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const repoURL = "https://github.com/koodeau/req"

// version is set at release time:
//
//	go build -ldflags "-X main.version=1.2.3"
//
// Builds made with `go install ...@vX.Y.Z` pick it up from the module
// info instead; anything else reports "dev".
var version = "dev"

func init() {
	if version != "dev" {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = strings.TrimPrefix(bi.Main.Version, "v")
	}
}

type config struct {
	method      string
	url         string
	items       []string
	form        bool
	rawData     string
	hasRawData  bool
	verbose     bool
	headersOnly bool
	bodyOnly    bool
	include     bool
	raw         bool
	noColor     bool
	forceColor  bool
	noFollow    bool
	insecure    bool
	fail        bool
	timeout     time.Duration
	output      string
	user        string
	bearer      string
}

func isMethod(s string) bool {
	switch strings.ToUpper(s) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	}
	return false
}

// parseArgs returns (nil, nil) when it already handled the invocation
// (--help, --version, or no arguments at all).
func parseArgs(args []string) (*config, error) {
	if len(args) == 0 {
		printHelp(os.Stdout, colorFor(os.Stdout, args))
		return nil, nil
	}

	c := &config{timeout: 30 * time.Second}
	var pos []string
	i := 0
	val := func(flag string) (string, error) {
		if i >= len(args) {
			return "", fmt.Errorf("the flag '%s' requires a value", flag)
		}
		v := args[i]
		i++
		return v, nil
	}
	for i < len(args) {
		a := args[i]
		i++
		var err error
		switch a {
		case "-h", "--help":
			printHelp(os.Stdout, colorFor(os.Stdout, args))
			return nil, nil
		case "--version":
			fmt.Println("req", version)
			return nil, nil
		case "-f", "--form":
			c.form = true
		case "-v", "--verbose":
			c.verbose = true
		case "-I", "--headers-only":
			c.headersOnly = true
		case "-b", "--body":
			c.bodyOnly = true
		case "-i", "--include":
			c.include = true
		case "--raw":
			c.raw = true
		case "--no-color":
			c.noColor = true
		case "--color":
			c.forceColor = true
		case "--no-follow":
			c.noFollow = true
		case "-k", "--insecure":
			c.insecure = true
		case "--fail":
			c.fail = true
		case "-d", "--data":
			c.rawData, err = val(a)
			c.hasRawData = true
		case "-H", "--header":
			var h string
			h, err = val(a)
			if err == nil {
				c.items = append(c.items, h)
			}
		case "-u", "--user":
			c.user, err = val(a)
		case "--bearer":
			c.bearer, err = val(a)
		case "-o", "--output":
			c.output, err = val(a)
		case "-t", "--timeout":
			var s string
			s, err = val(a)
			if err == nil {
				if n, convErr := strconv.ParseFloat(s, 64); convErr == nil {
					c.timeout = time.Duration(n * float64(time.Second))
				} else if d, durErr := time.ParseDuration(s); durErr == nil {
					c.timeout = d
				} else {
					err = fmt.Errorf("'%s' is not a valid timeout (try 10, 2.5, 500ms or 1m)", s)
				}
			}
		default:
			if strings.HasPrefix(a, "-") && len(a) > 1 {
				return nil, fmt.Errorf("unexpected flag '%s'", a)
			}
			pos = append(pos, a)
		}
		if err != nil {
			return nil, err
		}
	}

	if len(pos) > 0 && isMethod(pos[0]) {
		c.method = strings.ToUpper(pos[0])
		pos = pos[1:]
	}
	if len(pos) == 0 {
		return nil, fmt.Errorf("the following arguments are required: URL")
	}
	c.url = pos[0]
	c.items = append(c.items, pos[1:]...)
	return c, nil
}

func main() {
	args := os.Args[1:]
	c, err := parseArgs(args)
	if err != nil {
		printUsageError(os.Stderr, colorFor(os.Stderr, args), err.Error())
		os.Exit(2)
	}
	if c == nil {
		return
	}
	code, err := run(c)
	if err != nil {
		printRuntimeError(os.Stderr, colorFor(os.Stderr, args), err)
		os.Exit(1)
	}
	os.Exit(code)
}
