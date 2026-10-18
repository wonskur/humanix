package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// ValidateSHA256 validates a hexadecimal SHA256 digest.
func ValidateSHA256(expectedHex string) error {
	decoded, err := hex.DecodeString(strings.TrimSpace(expectedHex))
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("invalid SHA256 digest")
	}
	return nil
}

// VerifyFileSHA256 checks if the file at path matches the expected SHA256 hex digest.
func VerifyFileSHA256(path string, expectedHex string) error {
	if err := ValidateSHA256(expectedHex); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file %s for checksum verification: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to read file %s for hashing: %w", path, err)
	}

	actualHex := hex.EncodeToString(h.Sum(nil))
	if actualHex != strings.ToLower(strings.TrimSpace(expectedHex)) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", path, expectedHex, actualHex)
	}

	return nil
}

// ComputeSHA256 computes and returns the hex SHA256 digest of reader r.
func ComputeSHA256(ctx context.Context, r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("failed to compute sha256: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
