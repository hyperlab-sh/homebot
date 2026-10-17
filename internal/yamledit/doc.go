// Package yamledit changes one value in a YAML file and leaves every other
// byte alone: comments, blank lines, quoting, indentation and line endings
// stay as written. homebot uses it for decisions.yml and homebot.yml, which
// people edit by hand.
//
// An edit finds the target node with go-yaml's AST, splices new bytes into the
// source at that spot (or at the end of the parent mapping or sequence when the
// key is missing), then parses the result and checks the edit landed where it
// should. Block and flow styles are both supported, in LF and CRLF files.
package yamledit
