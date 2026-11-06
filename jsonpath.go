package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const maxIndex = 10000

var errConflict = errors.New("conflicts with a value set earlier (a key can't be both a value and an object/array)")

// seg is one step of a path like user.tags[0]: an object key, an array
// index, or an append ("[]").
type seg struct {
	key   string
	idx   int
	isIdx bool
	push  bool
}

// parsePath turns "user.tags[0].name" into segments.
// Use a backslash to write literal dots or brackets in a key: "a\.b".
func parsePath(s string) ([]seg, error) {
	var segs []seg
	var cur strings.Builder
	closed := false // previous token was a ']'

	pushKey := func() {
		segs = append(segs, seg{key: cur.String()})
		cur.Reset()
	}

	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '\\':
			if closed {
				return nil, fmt.Errorf("expected '.' or '[' after ']' in %q", s)
			}
			if i+1 < len(s) {
				cur.WriteByte(s[i+1])
				i += 2
			} else {
				cur.WriteByte(c)
				i++
			}
		case '.':
			if cur.Len() > 0 {
				pushKey()
			} else if !closed {
				return nil, fmt.Errorf("empty key segment in %q", s)
			}
			closed = false
			i++
		case '[':
			if cur.Len() > 0 {
				pushKey()
			} else if len(segs) == 0 {
				return nil, fmt.Errorf("path %q must start with a key", s)
			}
			end := strings.IndexByte(s[i:], ']')
			if end < 0 {
				return nil, fmt.Errorf("missing ']' in %q", s)
			}
			inner := s[i+1 : i+end]
			if inner == "" {
				segs = append(segs, seg{isIdx: true, push: true})
			} else {
				n, err := strconv.Atoi(inner)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("bad array index [%s] in %q", inner, s)
				}
				segs = append(segs, seg{isIdx: true, idx: n})
			}
			closed = true
			i += end + 1
		default:
			if closed {
				return nil, fmt.Errorf("expected '.' or '[' after ']' in %q", s)
			}
			cur.WriteByte(c)
			i++
		}
	}
	if cur.Len() > 0 {
		pushKey()
	} else if !closed {
		return nil, fmt.Errorf("empty key in %q", s)
	}
	return segs, nil
}

// jnode is a JSON tree node that keeps object keys in insertion order.
type jnode struct {
	obj, arr bool
	keys     []string
	kids     map[string]*jnode
	elems    []*jnode
	leaf     []byte // raw JSON for a value
}

func (n *jnode) child(s seg) (*jnode, error) {
	if s.isIdx {
		if n.obj || n.leaf != nil {
			return nil, errConflict
		}
		n.arr = true
		if s.push {
			c := &jnode{}
			n.elems = append(n.elems, c)
			return c, nil
		}
		if s.idx > maxIndex {
			return nil, fmt.Errorf("array index %d is too large (max %d)", s.idx, maxIndex)
		}
		for len(n.elems) <= s.idx {
			n.elems = append(n.elems, &jnode{})
		}
		return n.elems[s.idx], nil
	}
	if n.arr || n.leaf != nil {
		return nil, errConflict
	}
	n.obj = true
	if n.kids == nil {
		n.kids = map[string]*jnode{}
	}
	c, ok := n.kids[s.key]
	if !ok {
		c = &jnode{}
		n.kids[s.key] = c
		n.keys = append(n.keys, s.key)
	}
	return c, nil
}

func (n *jnode) set(segs []seg, raw []byte) error {
	cur := n
	for _, s := range segs {
		c, err := cur.child(s)
		if err != nil {
			return err
		}
		cur = c
	}
	if cur.obj || cur.arr {
		return errConflict
	}
	cur.leaf = raw
	return nil
}

func (n *jnode) write(b *bytes.Buffer) {
	switch {
	case n.obj:
		b.WriteByte('{')
		for i, k := range n.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(quote(k))
			b.WriteByte(':')
			n.kids[k].write(b)
		}
		b.WriteByte('}')
	case n.arr:
		b.WriteByte('[')
		for i, e := range n.elems {
			if i > 0 {
				b.WriteByte(',')
			}
			e.write(b)
		}
		b.WriteByte(']')
	case n.leaf != nil:
		b.Write(n.leaf)
	default:
		b.WriteString("null") // gap left by a sparse index like a[2]=x
	}
}
