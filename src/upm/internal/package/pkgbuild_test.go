package pkg

import (
	"context"
	"strings"
	"testing"
)

func TestParsePKGBUILDReader(t *testing.T) {
	pkgbuildData := `
# Maintainer: Arch User <user@example.com>
pkgname=yay
pkgver=12.3.5
pkgrel=2
pkgdesc="Yet another Yogurt - An AUR Helper written in Go"
url="https://github.com/Jguer/yay"
license=('GPL-3.0-or-later')
depends=(
    'pacman>6.1'
    'git'
)
makedepends=('go>=1.21')
`

	p, err := ParsePKGBUILDReader(context.Background(), strings.NewReader(pkgbuildData))
	if err != nil {
		t.Fatalf("ParsePKGBUILDReader failed: %v", err)
	}

	if p.Name != "yay" {
		t.Errorf("expected name 'yay', got %q", p.Name)
	}
	if p.Version != "12.3.5-2" {
		t.Errorf("expected version '12.3.5-2', got %q", p.Version)
	}
	if p.Provider != "aur" {
		t.Errorf("expected provider 'aur', got %q", p.Provider)
	}
	if p.URL != "https://github.com/Jguer/yay" {
		t.Errorf("expected URL 'https://github.com/Jguer/yay', got %q", p.URL)
	}
	if p.License != "GPL-3.0-or-later" {
		t.Errorf("expected license 'GPL-3.0-or-later', got %q", p.License)
	}

	if len(p.Depends) != 3 {
		t.Fatalf("expected 3 dependencies (2 runtime, 1 build), got %d", len(p.Depends))
	}

	if p.Depends[0].Name != "pacman" || p.Depends[0].Version != ">6.1" || p.Depends[0].Type != "runtime" {
		t.Errorf("unexpected dep 0: %+v", p.Depends[0])
	}
	if p.Depends[1].Name != "git" || p.Depends[1].Type != "runtime" {
		t.Errorf("unexpected dep 1: %+v", p.Depends[1])
	}
	if p.Depends[2].Name != "go>=1.21" || p.Depends[2].Type != "build" {
		t.Errorf("unexpected dep 2: %+v", p.Depends[2])
	}
}
