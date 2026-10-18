package pkg

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParsePKGBUILD reads an Arch Linux / AUR PKGBUILD file and extracts package metadata.
func ParsePKGBUILD(ctx context.Context, path string) (*Package, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open PKGBUILD at %s: %w", path, err)
	}
	defer file.Close()

	return ParsePKGBUILDReader(ctx, file)
}

// ParsePKGBUILDReader parses PKGBUILD contents from any io.Reader.
func ParsePKGBUILDReader(ctx context.Context, r io.Reader) (*Package, error) {
	scanner := bufio.NewScanner(r)
	vars := make(map[string]string)
	arrays := make(map[string][]string)

	var inArray bool
	var currentArrayName string
	var currentArrayItems []string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if inArray {
			if idx := strings.Index(line, ")"); idx != -1 {
				chunk := strings.TrimSpace(line[:idx])
				if chunk != "" {
					currentArrayItems = append(currentArrayItems, splitShellTokens(chunk)...)
				}
				arrays[currentArrayName] = currentArrayItems
				inArray = false
				currentArrayName = ""
				currentArrayItems = nil
			} else {
				currentArrayItems = append(currentArrayItems, splitShellTokens(line)...)
			}
			continue
		}

		// Check for assignment: key=val or key=(val1 val2)
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		if strings.HasPrefix(val, "(") {
			val = strings.TrimPrefix(val, "(")
			if idx := strings.Index(val, ")"); idx != -1 {
				// Single line array
				inside := strings.TrimSpace(val[:idx])
				arrays[key] = splitShellTokens(inside)
			} else {
				// Multi-line array start
				inArray = true
				currentArrayName = key
				currentArrayItems = splitShellTokens(val)
			}
			continue
		}

		vars[key] = cleanQuotes(val)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading PKGBUILD: %w", err)
	}

	name := vars["pkgname"]
	if name == "" && len(arrays["pkgname"]) > 0 {
		name = arrays["pkgname"][0]
	}

	if name == "" {
		return nil, fmt.Errorf("missing pkgname in PKGBUILD")
	}

	version := vars["pkgver"]
	if rel := vars["pkgrel"]; rel != "" {
		version = fmt.Sprintf("%s-%s", version, rel)
	}

	p := &Package{
		Name:        name,
		Version:     version,
		Provider:    "aur",
		Description: vars["pkgdesc"],
		URL:         vars["url"],
	}

	if len(arrays["license"]) > 0 {
		p.License = strings.Join(arrays["license"], ", ")
	} else if l := vars["license"]; l != "" {
		p.License = l
	}

	for _, depStr := range arrays["depends"] {
		depStr = strings.TrimSpace(depStr)
		if depStr == "" {
			continue
		}
		depName := depStr
		depVer := ""
		for _, op := range []string{">=", "<=", "=", ">", "<"} {
			if idx := strings.Index(depStr, op); idx != -1 {
				depName = strings.TrimSpace(depStr[:idx])
				depVer = strings.TrimSpace(depStr[idx:])
				break
			}
		}
		p.Depends = append(p.Depends, Dep{
			Name:    depName,
			Version: depVer,
			Type:    "runtime",
		})
	}

	for _, depStr := range arrays["makedepends"] {
		depStr = strings.TrimSpace(depStr)
		if depStr == "" {
			continue
		}
		p.Depends = append(p.Depends, Dep{
			Name: depStr,
			Type: "build",
		})
	}

	return p, nil
}

func cleanQuotes(s string) string {
	s = strings.TrimSpace(s)
	if (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) ||
		(strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) {
		return s[1 : len(s)-1]
	}
	return s
}

func splitShellTokens(s string) []string {
	var tokens []string
	var current strings.Builder
	var inSingleQuote, inDoubleQuote bool

	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
				continue
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
				continue
			}
		case ' ', '\t':
			if !inSingleQuote && !inDoubleQuote {
				if current.Len() > 0 {
					tokens = append(tokens, current.String())
					current.Reset()
				}
				continue
			}
		}
		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}
