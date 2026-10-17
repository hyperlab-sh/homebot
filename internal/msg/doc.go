// Package msg is every message homebot shows a person. The texts live in
// catalog.yaml; go generate turns each entry into a typed accessor
// (InboxHeaderCount(n)) returning a Text, which internal/voice renders.
// Code never writes user-facing English of its own.
package msg

//go:generate go run ../../tools/gencatalog
