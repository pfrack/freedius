//go:build mage

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

// Test runs unit tests with race detection and coverage.
func Test() error {
	return sh.RunV("go", "test", "-race", "-cover", "./...")
}

// Vet runs go vet.
func Vet() error {
	return sh.RunV("go", "vet", "./...")
}

// Build compiles the freedius binary.
func Build() error {
	return sh.RunV("go", "build", "-o", "freedius", ".")
}

// GenerateCheck ensures generated files are up to date.
func GenerateCheck() error {
	if err := sh.RunV("go", "generate", "./..."); err != nil {
		return err
	}
	return sh.RunV("git", "diff", "--exit-code", "--", "*.go")
}

// Tidy runs go mod tidy.
func Tidy() error {
	return sh.RunV("go", "mod", "tidy")
}

// Run starts the server, passing through extra args via ARGS env var.
func Run() error {
	args := []string{"run", "."}
	if extra := os.Getenv("ARGS"); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}
	return sh.RunV("go", args...)
}

// Verbose starts the server with verbose error output.
func Verbose() error {
	return sh.RunV("go", "run", ".", "--verbose-errors")
}

// LintStatic runs staticcheck, installing it if missing.
func LintStatic() error {
	if _, err := sh.Output("which", "staticcheck"); err != nil {
		if err := sh.RunV("go", "install", "honnef.co/go/tools/cmd/staticcheck@latest"); err != nil {
			return err
		}
	}
	return sh.RunV("staticcheck", "./...")
}

// LintGolangci runs golangci-lint. Warns and exits if not found.
func LintGolangci() error {
	if _, err := sh.Output("which", "golangci-lint"); err != nil {
		return sh.RunV("sh", "-c", "echo 'golangci-lint not found. Install: https://golangci-lint.run/usage/install/' && exit 1")
	}
	return sh.RunV("golangci-lint", "run", "./...")
}

// Lint runs all linters (vet + staticcheck + golangci-lint).
func Lint() error {
	mg.SerialDeps(Vet, LintStatic, LintGolangci)
	return nil
}

// CI runs the full CI pipeline: vet + generate-check + test + build.
func CI() error {
	mg.SerialDeps(Vet, GenerateCheck, Test, Build)
	return nil
}

// ManualTest runs the manual test script.
func ManualTest() error {
	return sh.RunV("./test-manual.sh")
}

// InstallHooks copies the pre-commit hook into .git/hooks/.
func InstallHooks() error {
	if err := sh.Copy(".git/hooks/pre-commit", "scripts/pre-commit"); err != nil {
		return err
	}
	return sh.RunV("chmod", "+x", ".git/hooks/pre-commit")
}

// InstallGoimports installs goimports if missing.
func InstallGoimports() error {
	if _, err := sh.Output("which", "goimports"); err != nil {
		return sh.RunV("go", "install", "golang.org/x/tools/cmd/goimports@latest")
	}
	return nil
}

// InstallGolines installs golines if missing.
func InstallGolines() error {
	if _, err := sh.Output("which", "golines"); err != nil {
		return sh.RunV("go", "install", "github.com/segmentio/golines@latest")
	}
	return nil
}

// InstallGci installs gci if missing.
func InstallGci() error {
	if _, err := sh.Output("which", "gci"); err != nil {
		return sh.RunV("go", "install", "github.com/daixiang0/gci@latest")
	}
	return nil
}

// Format runs all formatters on every .go file.
func Format() error {
	mg.Deps(InstallGoimports, InstallGolines, InstallGci)
	return filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if isVendoredOrGenerated(path) {
			return nil
		}
		if err := sh.RunV("gofmt", "-w", path); err != nil {
			return err
		}
		if err := sh.RunV("goimports", "-w", "-local", "github.com/pfrack/freedius", path); err != nil {
			return err
		}
		if err := sh.RunV("golines", "-w", path); err != nil {
			return err
		}
		return sh.RunV("gci", "write",
			"--skip-generated", "-s", "standard", "-s", "default",
			"-s", "prefix(github.com/pfrack/freedius)", "-s", "blank",
			"-s", "dot", "-s", "alias", "-s", "localmodule", path)
	})
}

// FormatChanged runs formatters only on changed Go files.
func FormatChanged() error {
	mg.Deps(InstallGoimports, InstallGolines, InstallGci)
	out, err := sh.Output("git", "diff", "--name-only", "--diff-filter=ACM", "HEAD")
	if err != nil {
		return err
	}
	untracked, _ := sh.Output("git", "ls-files", "--others", "--exclude-standard")
	files := strings.Fields(out + "\n" + untracked)
	for _, f := range files {
		if !strings.HasSuffix(f, ".go") || isVendoredOrGenerated(f) {
			continue
		}
		if err := sh.RunV("gofmt", "-w", f); err != nil {
			return err
		}
		if err := sh.RunV("goimports", "-w", "-local", "github.com/pfrack/freedius", f); err != nil {
			return err
		}
		if err := sh.RunV("golines", "-w", f); err != nil {
			return err
		}
		if err := sh.RunV("gci", "write",
			"--skip-generated", "-s", "standard", "-s", "default",
			"-s", "prefix(github.com/pfrack/freedius)", "-s", "blank",
			"-s", "dot", "-s", "alias", "-s", "localmodule", f); err != nil {
			return err
		}
	}
	return nil
}

// isVendoredOrGenerated returns true if the path is under vendor/ or
// magefiles/ or is a generated file.
func isVendoredOrGenerated(path string) bool {
	if strings.HasPrefix(path, "vendor/") {
		return true
	}
	if strings.HasPrefix(path, "magefiles/") {
		return true
	}
	return false
}
