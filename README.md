# req

**A tiny HTTP client for the terminal.** Test websites and backends with short, readable commands and get colorful, formatted responses back. Think of it as a simplified Postman that lives in your shell.

```console
$ req POST :3000/users user.name=John user.address.city=Amsterdam roles[]=admin active:=true

HTTP/1.1 201 Created  · 12ms · 96 B
Content-Type: application/json
Date: Fri, 06 Nov 2026 12:00:00 GMT

{
  "id": 42,
  "user": {
    "name": "John",
    "address": {
      "city": "Amsterdam"
    }
  },
  "roles": [
    "admin"
  ],
  "active": true
}
```

> **Status:** early development (v0.1.0). The core request/response workflow works; saved requests and environments are on the [roadmap](#roadmap).

---

## Contents

- [req](#req)
  - [Contents](#contents)
  - [Features](#features)
  - [Install](#install)
  - [Quick start](#quick-start)
  - [URLs](#urls)
  - [Request items](#request-items)
  - [Nested JSON](#nested-json)
  - [Forms and file uploads](#forms-and-file-uploads)
  - [Raw bodies and stdin](#raw-bodies-and-stdin)
  - [Authentication](#authentication)
  - [Output](#output)
  - [Flags reference](#flags-reference)
  - [Exit codes](#exit-codes)
  - [Recipes](#recipes)
  - [Shell quoting tips](#shell-quoting-tips)
  - [Roadmap](#roadmap)
  - [Project layout](#project-layout)

---

## Features

- **Short syntax.** Headers, query parameters, JSON fields and file uploads are all plain arguments, with no quoting gymnastics and no hand-written JSON.
- **Beautiful output.** Syntax-colored JSON (key order preserved), colored HTML and XML, and a status line with timing and size.
- **Nested JSON from the command line.** `user.address.city=Amsterdam` and `roles[]=admin` just work.
- **All the methods you need.** `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`.
- **Forms and uploads.** URL-encoded forms and multipart file uploads.
- **Pipe-friendly.** When output is piped you get just the body, so `req ... | jq` behaves.
- **Friendly errors.** Usage errors show the usage line, and network errors come with a hint (for example, "is the server running?").
- **No dependencies.** Standard library only, one static binary.

## Install

With Go 1.21 or newer:

```sh
go install github.com/koodeau/req@latest
```

Or build from source:

```sh
git clone https://github.com/koodeau/req.git
cd req
go build -o req .
mv req /usr/local/bin/   # or anywhere on your PATH
```

Check it works:

```sh
req --version
req --help
```

## Quick start

```sh
# GET a page or an API
req example.com
req :3000/api/users

# POST JSON (the method becomes POST automatically when there is a body)
req :3000/api/users name=John age:=30

# Be explicit about the method
req PUT :3000/api/users/1 name=John
req DELETE :3000/api/users/1

# Query string, headers and auth
req api.example.com/search q==golang page==2 Accept:application/json --bearer "$TOKEN"
```

The general shape is:

```
req [METHOD] URL [ITEM...] [FLAGS]
```

Flags can appear anywhere on the line.

## URLs

You don't have to type the scheme:

| You type             | `req` requests                 |
| -------------------- | ------------------------------ |
| `:3000/api`          | `http://localhost:3000/api`    |
| `localhost:3000/api` | `http://localhost:3000/api`    |
| `127.0.0.1:8080`     | `http://127.0.0.1:8080`        |
| `example.com/path`   | `https://example.com/path`     |
| `http://example.com` | used exactly as written        |

Anything pointing at `localhost`, `127.*`, `0.0.0.0` or `[::1]` defaults to `http`; everything else defaults to `https`.

## Request items

Everything after the URL that isn't a flag is a **request item**. The first separator in the item decides what it is:

| Item          | Meaning                                          | Example                      |
| ------------- | ------------------------------------------------ | ---------------------------- |
| `Name:Value`  | Request header                                   | `Authorization:"Bearer abc"` |
| `key==value`  | Query string parameter                           | `page==2`                    |
| `key=value`   | Body field, sent as a string                     | `name=John`                  |
| `key:=<json>` | Body field with a raw JSON value                 | `age:=30` `tags:='["a","b"]'` |
| `key@path`    | File upload (switches to `multipart/form-data`)  | `avatar@./me.png`            |

By default, body fields are sent as a JSON object with `Content-Type: application/json`. Use `:=` whenever the value is not a string: numbers, booleans, `null`, arrays and objects.

```sh
req POST :3000/items name=Widget price:=9.99 in_stock:=true tags:='["new","sale"]' meta:='{"color":"red"}'
```

The request above sends:

```json
{"name":"Widget","price":9.99,"in_stock":true,"tags":["new","sale"],"meta":{"color":"red"}}
```

## Nested JSON

Keys in a JSON body can be paths. Dots create objects and brackets create arrays.

| You type                        | Body sent                                  |
| ------------------------------- | ------------------------------------------ |
| `user.name=John`                | `{"user":{"name":"John"}}`                 |
| `user.address.city=Amsterdam`   | `{"user":{"address":{"city":"Amsterdam"}}}` |
| `roles[]=admin roles[]=dev`     | `{"roles":["admin","dev"]}`                |
| `tags[0]=go tags[1]=cli`        | `{"tags":["go","cli"]}`                    |
| `items[0].id:=1 items[0].qty:=2`| `{"items":[{"id":1,"qty":2}]}`             |
| `a\.b=1`                        | `{"a.b":1}`                                |

Details:

- Keys keep the order in which you typed them.
- `[]` appends to an array; `[n]` sets a specific index. Skipped indexes are filled with `null`, and indexes above 10000 are rejected.
- Use a backslash to write a literal `.` or `[` in a key: `a\.b`. In most shells you'll need quotes around it (`'a\.b=1'`).
- Setting the same path as both a value and an object (`a=1` and `a.b=2`) is an error rather than a silently wrong body.
- Nesting applies to **JSON bodies only**. Form and multipart fields stay flat, so `a.b` there is a literal field name.

## Forms and file uploads

Add `-f` / `--form` to send fields as `application/x-www-form-urlencoded`:

```sh
req POST :3000/login -f email=me@example.com password=secret
```

Any `key@path` item switches the request to `multipart/form-data`. Plain fields in the same command are sent as form parts:

```sh
req POST :3000/upload title="My cat" photo@./cat.png
```

## Raw bodies and stdin

Use `-d` / `--data` to send an exact body. Prefix a path with `@` to read the body from a file:

```sh
req POST :3000/echo -d '{"hello":"world"}'
req POST :3000/import -d @payload.json
```

If the body looks like a JSON object or array, `Content-Type: application/json` is set for you. Override it with a header: `Content-Type:text/plain`.

You can also pipe a body in. This works when you name a method that normally carries a body, such as `POST`, `PUT` or `PATCH`:

```sh
cat payload.json | req POST :3000/import
```

## Authentication

```sh
req :3000/me --bearer "$TOKEN"            # Authorization: Bearer <token>
req :3000/me -u john:s3cret                # HTTP basic auth
req :3000/me Authorization:"Token abc123"  # any scheme, via a header
```

## Output

For a terminal, `req` prints:

1. a status line, colored by class (2xx green, 3xx yellow, 4xx red, 5xx magenta) with the response time and size,
2. the response headers, sorted,
3. the body, formatted according to its type.

| Body type | What you get                                                        |
| --------- | ------------------------------------------------------------------- |
| JSON      | Pretty-printed and syntax-colored, with key order preserved         |
| HTML, XML | Syntax-colored (tags, attributes, values, comments); not re-indented yet |
| Text      | Printed as is                                                       |
| Binary    | A short placeholder; use `-o file` to save it                       |

Type detection uses the `Content-Type` header and falls back to sniffing the body.

**Piping.** When stdout isn't a terminal, `req` prints only the body (still pretty-printed, but without colors). Add `-i` to include the status line and headers.

**Colors.** Colors are on for terminals and off for pipes. Set the `NO_COLOR` environment variable to disable them, or use `--color` / `--no-color` to override everything. Help and error output follow the same rules.

**Seeing the request.** `-v` prints the request that was sent (method, path, headers and body) before the response, which is handy for checking what a nested body or a multipart upload really looks like.

## Flags reference

| Flag                        | Description                                                     |
| --------------------------- | --------------------------------------------------------------- |
| `-f`, `--form`              | Send fields as `application/x-www-form-urlencoded`              |
| `-d`, `--data <body>`       | Raw request body (`@file` reads it from a file)                 |
| `-H`, `--header <Name:Value>` | Add a request header (same as a `Name:Value` item)            |
| `-u`, `--user <user:pass>`  | HTTP basic authentication                                       |
| `--bearer <token>`          | Send `Authorization: Bearer <token>`                            |
| `-v`, `--verbose`           | Also print the request                                          |
| `-b`, `--body`              | Print only the response body                                    |
| `-I`, `--headers-only`      | Print only the status line and response headers                 |
| `-i`, `--include`           | Show status and headers even when piped                         |
| `--raw`                     | Don't pretty-print the body                                     |
| `-o`, `--output <file>`     | Save the response body to a file                                |
| `-t`, `--timeout <secs>`    | Timeout; default 30 seconds. `500ms` and `2m` also work         |
| `--no-follow`               | Don't follow redirects                                          |
| `-k`, `--insecure`          | Skip TLS certificate verification                               |
| `--fail`                    | Exit with status 1 on HTTP 4xx/5xx                              |
| `--color`, `--no-color`     | Force colors on or off                                          |
| `-h`, `--help`              | Show help                                                       |
| `--version`                 | Print the version                                               |

> `-I` means "headers only" here; it does **not** switch the method to `HEAD` the way it does in curl. Use `req HEAD URL` for that.

## Exit codes

| Code | Meaning                                                               |
| ---- | --------------------------------------------------------------------- |
| `0`  | The request completed (any HTTP status, unless `--fail` is set)       |
| `1`  | A network or runtime error, or `--fail` with a 4xx/5xx response       |
| `2`  | Usage error (bad flag, missing URL, unparsable item)                  |

## Recipes

**Log in, then use the token**

```sh
TOKEN=$(req POST :3000/login email=me@example.com password=secret | jq -r .token)
req :3000/me --bearer "$TOKEN"
```

**Fail a script when the API misbehaves**

```sh
req --fail -b api.example.com/health > /dev/null || echo "API is down"
```

**Download a file**

```sh
req https://example.com/report.pdf -o report.pdf
```

**Inspect redirects without following them**

```sh
req --no-follow -I http://example.com
```

**Check the exact request on the wire**

```sh
req -v POST :3000/users user.name=John roles[]=admin
```

**Talk to a dev server with a self-signed certificate**

```sh
req -k https://localhost:8443/health
```

## Shell quoting tips

Your shell sees the command before `req` does, and some characters mean something to it:

- **Brackets and globbing.** In zsh, `roles[]=admin` can fail with `no matches found`. Quote the item (`'roles[]=admin'`) or use explicit indexes (`roles[0]=admin`).
- **Values with spaces.** Quote them: `title="My cat"`.
- **Raw JSON values.** Wrap in single quotes: `tags:='["a","b"]'`.
- **Backslashes.** `a\.b=1` may lose its backslash. Write `'a\.b=1'`.
- **`&` in URLs.** Quote the URL, or use `key==value` items instead of building the query string by hand.

## Roadmap

- [ ] Saved requests and environments (`req.yaml`, `req run login`, `{{base_url}}` variables)
- [ ] Re-indented HTML and XML output
- [ ] Cookie jar / session mode
- [ ] Response assertions for scripting (status, JSON paths)
- [ ] Shell completions
- [ ] Unit tests for the path parser and body builder
- [ ] Release binaries

Ideas and bug reports are welcome on the [issue tracker](https://github.com/koodeau/req/issues).

## Project layout

```
.
├── main.go      # flag parsing and entry point
├── request.go   # items → HTTP request, sending, bodies (JSON, form, multipart)
├── jsonpath.go  # nested key paths and the ordered JSON tree
├── render.go    # colors and response/body formatting
├── help.go      # colored help, usage errors and runtime errors
├── go.mod
└── README.md
```

---

Made by [koodeau](https://github.com/koodeau).
