//go:build mage

package main

import (
	"fmt"
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
	return sh.RunV("go", "build", "-o", "freedius", "./cmd/freedius")
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
	args := []string{"run", "./cmd/freedius"}
	if extra := os.Getenv("ARGS"); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}
	return sh.RunV("go", args...)
}

// Verbose starts the server with verbose error output.
func Verbose() error {
	return sh.RunV("go", "run", "./cmd/freedius", "--verbose-errors")
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
		msg := "golangci-lint not found. Install: https://golangci-lint.run/usage/install/"
		fmt.Fprintln(os.Stderr, msg)
		return fmt.Errorf(msg)
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
	hooksPath, err := sh.Output("git", "rev-parse", "--git-path", "hooks")
	if err != nil {
		return fmt.Errorf("resolve git hooks path: %w", err)
	}
	dst := strings.TrimSpace(hooksPath) + "/pre-commit"
	if err := os.Symlink("../../scripts/pre-commit", dst); err != nil {
		if err := sh.Copy(dst, "scripts/pre-commit"); err != nil {
			return err
		}
	}
	return sh.RunV("chmod", "+x", dst)
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
		return sh.RunV("go", "install", "github.com/daixiang0/gci@v0.13.5")
	}
	return nil
}

// Format runs all formatters on every .go file.
func Format() error {
	mg.Deps(InstallGoimports, InstallGolines, InstallGci)
	var files []string
	if err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if !isVendoredOrGenerated(path) {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return err
	}
	return formatFiles(files)
}

// FormatChanged runs formatters only on changed Go files.
func FormatChanged() error {
	mg.Deps(InstallGoimports, InstallGolines, InstallGci)
	diffTarget := "HEAD"
	if _, err := sh.Output("git", "rev-parse", "--verify", "HEAD"); err != nil {
		diffTarget = "4b825dc642cb6eb9a060e54bf899d153036e1e3b"
	}
	out, err := sh.Output("git", "diff", "--name-only", "--diff-filter=ACM", diffTarget)
	if err != nil {
		return err
	}
	untracked, err := sh.Output("git", "ls-files", "--others", "--exclude-standard")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: git ls-files failed: %v\n", err)
	}
	allFiles := strings.Fields(out + "\n" + untracked)
	var files []string
	for _, f := range allFiles {
		if strings.HasSuffix(f, ".go") && !isVendoredOrGenerated(f) {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil
	}
	return formatFiles(files)
}

// formatFiles runs the formatting toolchain on the given Go files.
func formatFiles(files []string) error {
	args := make([]string, 0, len(files)+1)
	args = append(args, "-w")
	args = append(args, files...)
	if err := sh.RunV("gofmt", args...); err != nil {
		return err
	}
	local := []string{"-w", "-local", "github.com/pfrack/freedius"}
	local = append(local, files...)
	if err := sh.RunV("goimports", local...); err != nil {
		return err
	}
	lineArgs := make([]string, 0, len(files)+1)
	lineArgs = append(lineArgs, "-w")
	lineArgs = append(lineArgs, files...)
	if err := sh.RunV("golines", lineArgs...); err != nil {
		return err
	}
	gciArgs := []string{"write",
		"--skip-generated", "-s", "standard", "-s", "default",
		"-s", "prefix(github.com/pfrack/freedius)", "-s", "blank",
		"-s", "dot", "-s", "alias", "-s", "localmodule"}
	gciArgs = append(gciArgs, files...)
	return sh.RunV("gci", gciArgs...)
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
