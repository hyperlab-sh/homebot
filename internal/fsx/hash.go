// Package fsx holds the file helpers every package shares: atomic writes,
// JSON state files, hashes and directory walks.
package fsx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// Sha256 returns the hex SHA-256 of data.
func Sha256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// FileSha256 returns the hex SHA-256 of the file at path.
func FileSha256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return Sha256(data), nil
}
