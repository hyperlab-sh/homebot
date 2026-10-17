package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// OrderedMap is a string-keyed map that remembers insertion order, because
// JSON.stringify writes an object's keys in the order they were added and the
// state files must come out the way the TypeScript wrote them. The zero value
// is an empty map ready to use. Hold it by pointer or as a struct field: a
// copy shares the values but not the key list, so the two drift apart.
type OrderedMap[V any] struct {
	keys []string
	vals map[string]V
}

// Get returns the value for key.
func (m *OrderedMap[V]) Get(key string) (V, bool) {
	v, ok := m.vals[key]
	return v, ok
}

// Set stores v under key. A new key goes last; an existing key keeps its place.
func (m *OrderedMap[V]) Set(key string, v V) {
	if m.vals == nil {
		m.vals = map[string]V{}
	}
	if _, ok := m.vals[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.vals[key] = v
}

// Delete removes key. Setting it again later puts it last, as in JavaScript.
func (m *OrderedMap[V]) Delete(key string) {
	if _, ok := m.vals[key]; !ok {
		return
	}
	delete(m.vals, key)
	for i, k := range m.keys {
		if k == key {
			m.keys = append(m.keys[:i:i], m.keys[i+1:]...)
			break
		}
	}
}

// Len is the number of keys.
func (m *OrderedMap[V]) Len() int { return len(m.keys) }

// Keys returns the keys in insertion order.
func (m *OrderedMap[V]) Keys() []string { return append([]string(nil), m.keys...) }

// MarshalJSON writes the keys in insertion order; an empty map is {}.
func (m OrderedMap[V]) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := writeKey(&buf, k); err != nil {
			return nil, err
		}
		v, err := marshalNoEscape(m.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON reads an object, keeping the key order of the text. A repeated
// key keeps its first position and its last value, as JSON.parse does.
func (m *OrderedMap[V]) UnmarshalJSON(data []byte) error {
	if string(bytes.TrimSpace(data)) == "null" {
		return nil
	}
	keys, raws, err := decodeObject(data)
	if err != nil {
		return err
	}
	*m = OrderedMap[V]{}
	for _, k := range keys {
		var v V
		if err := json.Unmarshal(raws[k], &v); err != nil {
			return err
		}
		m.Set(k, v)
	}
	return nil
}

// decodeObject splits a JSON object into its keys, in text order, and the raw
// value of each.
func decodeObject(data []byte) ([]string, map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, nil, errors.New("not a JSON object")
	}
	var keys []string
	raws := map[string]json.RawMessage{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		k := kt.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, nil, err
		}
		if _, seen := raws[k]; !seen {
			keys = append(keys, k)
		}
		raws[k] = raw
	}
	if _, err := dec.Token(); err != nil {
		return nil, nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, nil, fmt.Errorf("trailing data after JSON object")
	}
	return keys, raws, nil
}

func writeKey(buf *bytes.Buffer, k string) error {
	b, err := marshalNoEscape(k)
	if err != nil {
		return err
	}
	buf.Write(b)
	buf.WriteByte(':')
	return nil
}

// marshalNoEscape is json.Marshal without HTML escaping, compact, no newline.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
