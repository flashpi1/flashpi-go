package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// node is a JSON value that keeps the order of object members, which encoding/json's maps lose:
// the generated fields follow the order of the properties in the document.
type node struct {
	kind    kind
	members []member // objects
	items   []*node  // arrays
	text    string   // strings, and numbers as written
	boolean bool
}

type kind int

const (
	kindNull kind = iota
	kindBool
	kindNumber
	kindString
	kindArray
	kindObject
)

type member struct {
	key   string
	value *node
}

func parseJSON(data []byte) (*node, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	root, err := parseValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after the JSON document")
	}
	return root, nil
}

func parseValue(decoder *json.Decoder) (*node, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case nil:
		return &node{kind: kindNull}, nil
	case bool:
		return &node{kind: kindBool, boolean: value}, nil
	case json.Number:
		return &node{kind: kindNumber, text: value.String()}, nil
	case string:
		return &node{kind: kindString, text: value}, nil
	case json.Delim:
		switch value {
		case '{':
			result := &node{kind: kindObject}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("an object key is not a string")
				}
				if result.get(key) != nil {
					return nil, fmt.Errorf("the key %q appears twice in one object", key)
				}
				child, err := parseValue(decoder)
				if err != nil {
					return nil, err
				}
				result.members = append(result.members, member{key, child})
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return result, nil
		case '[':
			result := &node{kind: kindArray}
			for decoder.More() {
				child, err := parseValue(decoder)
				if err != nil {
					return nil, err
				}
				result.items = append(result.items, child)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return result, nil
		}
	}
	return nil, fmt.Errorf("unexpected JSON token %v", token)
}

// get returns the member key of an object, or nil.
func (n *node) get(key string) *node {
	if n == nil || n.kind != kindObject {
		return nil
	}
	for _, m := range n.members {
		if m.key == key {
			return m.value
		}
	}
	return nil
}

func (n *node) has(key string) bool { return n.get(key) != nil }

// children returns the items of an array, or nil for anything else.
func (n *node) children() []*node {
	if n == nil || n.kind != kindArray {
		return nil
	}
	return n.items
}

// entries returns the members of an object, or nil for anything else.
func (n *node) entries() []member {
	if n == nil || n.kind != kindObject {
		return nil
	}
	return n.members
}

func (n *node) keys() []string {
	if n == nil || n.kind != kindObject {
		return nil
	}
	keys := make([]string, 0, len(n.members))
	for _, m := range n.members {
		keys = append(keys, m.key)
	}
	return keys
}

// str returns the text of a string node, or "" for anything else.
func (n *node) str() string {
	if n == nil || n.kind != kindString {
		return ""
	}
	return n.text
}

// strings returns the texts of an array of strings, failing on any other item.
func (n *node) strings(where string) ([]string, error) {
	if n == nil {
		return nil, nil
	}
	if n.kind != kindArray {
		return nil, fmt.Errorf("%s: expected an array", where)
	}
	values := make([]string, 0, len(n.items))
	for _, item := range n.items {
		if item.kind != kindString {
			return nil, fmt.Errorf("%s: expected strings", where)
		}
		values = append(values, item.text)
	}
	return values, nil
}

// literal is the JSON text of a scalar node, for messages and docs.
func (n *node) literal() string {
	switch n.kind {
	case kindNull:
		return "null"
	case kindBool:
		if n.boolean {
			return "true"
		}
		return "false"
	case kindString:
		quoted, _ := json.Marshal(n.text)
		return string(quoted)
	case kindNumber:
		return n.text
	}
	return "…"
}
