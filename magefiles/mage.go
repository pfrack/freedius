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

// Tool versions - single source of truth
const (
	toolVersionStaticcheck  = "v0.7.0"
	toolVersionGolangciLint = "v2.12.2"
	toolVersionGovulncheck  = "v1.3.0"
	toolVersionGoimports    = "v0.47.0"
	toolVersionGolines      = "v0.12.2"
	toolVersionGci          = "v0.13.5"
)

// Build configuration
const (
	binaryName = "freedius"
	mainPath   = "./cmd/freedius"
)

// Default target when running 'mage' without arguments
var Default = Help

// Help displays available targets with descriptions organized by category.
func Help() {
	fmt.Println("Mage Targets for freedius")
	fmt.Println("=" + strings.Repeat("=", 50))
	fmt.Println()

	fmt.Println("Build & Install:")
	fmt.Println("  build           - Compile the freedius binary")
	fmt.Println("  install         - Install binary to $GOPATH/bin")
	fmt.Println("  clean           - Remove build artifacts and temporary files")
	fmt.Println()

	fmt.Println("Development:")
	fmt.Println("  run             - Start server (use ARGS env for flags)")
	fmt.Println("  verbose         - Start server with verbose error output")
	fmt.Println("  watch           - Auto-rebuild and restart on file changes")
	fmt.Println()

	fmt.Println("Testing & Quality:")
	fmt.Println("  test            - Run unit tests with race detection")
	fmt.Println("  benchmark       - Run performance benchmarks")
	fmt.Println("  coverage        - Generate HTML coverage report")
	fmt.Println("  manualTest      - Run manual test script")
	fmt.Println()

	fmt.Println("Linting & Security:")
	fmt.Println("  vet             - Run go vet")
	fmt.Println("  lint            - Run all linters (vet + staticcheck + golangci-lint)")
	fmt.Println("  lintStatic      - Run staticcheck only")
	fmt.Println("  lintGolangci    - Run golangci-lint only")
	fmt.Println("  govulncheck     - Check for known vulnerabilities")
	fmt.Println()

	fmt.Println("Code Quality:")
	fmt.Println("  format          - Format all Go files")
	fmt.Println("  formatChanged   - Format only changed Go files")
	fmt.Println("  tidy            - Run go mod tidy")
	fmt.Println("  modVerify       - Verify go.mod/go.sum integrity")
	fmt.Println("  generateCheck   - Ensure generated files are up to date")
	fmt.Println()

	fmt.Println("Docker:")
	fmt.Println("  dockerBuild     - Build Docker image")
	fmt.Println("  dockerRun       - Run Docker container")
	fmt.Println("  dockerPush      - Push Docker image to registry")
	fmt.Println()

	fmt.Println("Git Hooks & CI:")
	fmt.Println("  installHooks    - Install pre-commit Git hooks")
	fmt.Println("  ci              - Run full CI pipeline")
	fmt.Println()

	fmt.Println("Usage examples:")
	fmt.Println("  mage build")
	fmt.Println("  mage test")
	fmt.Println("  ARGS='--verbose-errors' mage run")
	fmt.Println("  COVERPROFILE=coverage.out mage test")
}

// Test runs unit tests with race detection and coverage.
// Set COVERPROFILE env var (e.g. "coverage.out") to write a coverage profile.
func Test() error {
	fmt.Println("→ Running tests with race detection...")
	args := []string{"test", "-race", "-cover"}
	if out := os.Getenv("COVERPROFILE"); out != "" {
		args = append(args, "-coverprofile="+out)
		fmt.Printf("  Coverage profile will be written to: %s\n", out)
	}
	args = append(args, "./...")
	return sh.RunV("go", args...)
}

// Benchmark runs performance benchmarks.
func Benchmark() error {
	fmt.Println("→ Running benchmarks...")
	return sh.RunV("go", "test", "-bench=.", "-benchmem", "./...")
}

// Coverage generates an HTML coverage report and opens it in the browser.
func Coverage() error {
	fmt.Println("→ Generating coverage report...")
	coverFile := "coverage.out"
	htmlFile := "coverage.html"

	if err := sh.RunV("go", "test", "-coverprofile="+coverFile, "./..."); err != nil {
		return fmt.Errorf("test coverage failed: %w", err)
	}

	if err := sh.RunV("go", "tool", "cover", "-html="+coverFile, "-o", htmlFile); err != nil {
		return fmt.Errorf("generate HTML coverage: %w", err)
	}

	fmt.Printf("✓ Coverage report generated: %s\n", htmlFile)
	fmt.Println("  Open it in your browser to view detailed coverage")
	return nil
}

// Vet runs go vet.
func Vet() error {
	fmt.Println("→ Running go vet...")
	return sh.RunV("go", "vet", "./...")
}

// Build compiles the freedius binary.
func Build() error {
	fmt.Println("→ Building freedius binary...")
	return sh.RunV("go", "build", "-o", "freedius", "./cmd/freedius")
}

// Install builds and installs the binary to $GOPATH/bin.
func Install() error {
	fmt.Println("→ Installing freedius to $GOPATH/bin...")
	return sh.RunV("go", "install", "./cmd/freedius")
}

// Clean removes build artifacts and temporary files.
func Clean() error {
	fmt.Println("→ Cleaning build artifacts...")

	artifacts := []string{
		"freedius",
		"coverage.out",
		"coverage.html",
	}

	for _, f := range artifacts {
		if err := sh.Rm(f); err != nil {
			// Ignore errors if file doesn't exist
			continue
		}
		fmt.Printf("  Removed: %s\n", f)
	}

	fmt.Println("✓ Clean complete")
	return nil
}

// GenerateCheck ensures generated files are up to date.
func GenerateCheck() error {
	fmt.Println("→ Checking generated files are up to date...")
	if err := sh.RunV("go", "generate", "./..."); err != nil {
		return err
	}
	return sh.RunV("git", "diff", "--exit-code", "--", "*.go")
}

// Tidy runs go mod tidy.
func Tidy() error {
	fmt.Println("→ Running go mod tidy...")
	return sh.RunV("go", "mod", "tidy")
}

// ModVerify verifies go.mod and go.sum integrity.
func ModVerify() error {
	fmt.Println("→ Verifying module dependencies...")
	if err := sh.RunV("go", "mod", "verify"); err != nil {
		return fmt.Errorf("module verification failed: %w", err)
	}
	fmt.Println("✓ All modules verified")
	return nil
}

// TidyCheck verifies that go.mod and go.sum are tidy.
func TidyCheck() error {
	fmt.Println("→ Checking go.mod tidiness...")
	if err := sh.RunV("go", "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}
	diff, _ := sh.Output("git", "diff", "--", "go.mod", "go.sum")
	sh.RunV("git", "checkout", "--", "go.mod", "go.sum")
	if diff != "" {
		return fmt.Errorf("go.mod/go.sum are not tidy:\n%s", diff)
	}
	return nil
}

// Run starts the server, passing through extra args via ARGS env var.
func Run() error {
	mg.Deps(Build)
	args := []string{"./freedius"}
	if extra := os.Getenv("ARGS"); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}
	return sh.RunV(args[0], args[1:]...)
}

// RunDev starts the server with go run, passing through extra args via ARGS env var.
func RunDev() error {
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

// Watch watches for file changes and rebuilds automatically.
func Watch() error {
	fmt.Println("→ Watching for changes... (Press Ctrl+C to stop)")
	fmt.Println("  Watching: *.go files")
	fmt.Println()

	// Initial build
	if err := Build(); err != nil {
		fmt.Printf("✗ Initial build failed: %v\n", err)
	} else {
		fmt.Println("✓ Initial build successful")
	}

	// Simple watch loop - rebuild on any .go file change
	lastCheck := ""
	for {
		out, err := sh.Output(
			"find",
			".",
			"-name",
			"*.go",
			"-type",
			"f",
			"-newer",
			"freedius",
			"-o",
			"-name",
			"*.go",
			"-type",
			"f",
			"!",
			"-path",
			"*/vendor/*",
			"!",
			"-path",
			"*/.*",
		)
		if err == nil && out != "" && out != lastCheck {
			lastCheck = out
			fmt.Println("\n→ Change detected, rebuilding...")
			if err := Build(); err != nil {
				fmt.Printf("✗ Build failed: %v\n", err)
			} else {
				fmt.Println("✓ Build successful")
			}
		}
		sh.RunV("sleep", "1")
	}
}

// LintStatic runs staticcheck, installing it if missing.
func LintStatic() error {
	if _, err := sh.Output("which", "staticcheck"); err != nil {
		if err := sh.RunV("go", "install", "honnef.co/go/tools/cmd/staticcheck@"+toolVersionStaticcheck); err != nil {
			return err
		}
	}
	return sh.RunV("staticcheck", "./...")
}

// LintGolangci runs golangci-lint, installing it if missing.
func LintGolangci() error {
	if _, err := sh.Output("which", "golangci-lint"); err != nil {
		if err := sh.RunV(
			"go",
			"install",
			"github.com/golangci/golangci-lint/v2/cmd/golangci-lint@"+toolVersionGolangciLint,
		); err != nil {
			return err
		}
	}
	return sh.RunV("golangci-lint", "run", "./...")
}

// Lint runs all linters (staticcheck + golangci-lint).
func Lint() error {
	mg.SerialDeps(LintStatic, LintGolangci)
	return nil
}

// Govulncheck runs govulncheck, installing it if missing.
func Govulncheck() error {
	if _, err := sh.Output("which", "govulncheck"); err != nil {
		if err := sh.RunV("go", "install", "golang.org/x/vuln/cmd/govulncheck@"+toolVersionGovulncheck); err != nil {
			return err
		}
	}
	return sh.RunV("govulncheck", "./...")
}

// FormatCheck verifies that all Go files are properly formatted.
func FormatCheck() error {
	fmt.Println("→ Checking formatting...")
	return sh.RunV("golangci-lint", "fmt", "--diff")
}

// CI runs the full CI pipeline: vet + generate-check + mod-verify + tidy-check + format-check + test + lint + build + govulncheck.
func CI() error {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"Vet", Vet},
		{"Mod Verify", ModVerify},
		{"Tidy Check", TidyCheck},
		{"Generate Check", GenerateCheck},
		{"Format Check", FormatCheck},
		{"Test", Test},
		{"Lint", Lint},
		{"Build", Build},
		{"Govulncheck", Govulncheck},
	}

	fmt.Println("→ Running CI Pipeline")
	fmt.Println()

	for i, step := range steps {
		fmt.Printf("[%d/%d] Running %s...\n", i+1, len(steps), step.name)
		if err := step.fn(); err != nil {
			fmt.Printf("✗ CI failed at step: %s\n", step.name)
			return fmt.Errorf("%s failed: %w", step.name, err)
		}
		fmt.Printf("✓ %s passed\n\n", step.name)
	}

	fmt.Println("✓ All CI checks passed!")
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
		return sh.RunV("go", "install", "golang.org/x/tools/cmd/goimports@"+toolVersionGoimports)
	}
	return nil
}

// InstallGolines installs golines if missing.
func InstallGolines() error {
	if _, err := sh.Output("which", "golines"); err != nil {
		return sh.RunV("go", "install", "github.com/segmentio/golines@"+toolVersionGolines)
	}
	return nil
}

// InstallGci installs gci if missing.
func InstallGci() error {
	if _, err := sh.Output("which", "gci"); err != nil {
		return sh.RunV("go", "install", "github.com/daixiang0/gci@"+toolVersionGci)
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

// Help2 displays detailed help information with sections and tool versions.
func Help2() {
	fmt.Println("Freedius Mage Build Targets")
	fmt.Println("============================")
	fmt.Println()

	sections := []struct {
		name    string
		targets []struct {
			name string
			desc string
		}
	}{
		{
			name: "Development",
			targets: []struct{ name, desc string }{
				{"run", "Start the server (use ARGS env var for extra arguments)"},
				{"verbose", "Start the server with verbose error output"},
				{"build", "Compile the freedius binary"},
				{"install", "Install the binary to $GOPATH/bin or /usr/local/bin"},
				{"clean", "Remove build artifacts and temporary files"},
			},
		},
		{
			name: "Testing & Quality",
			targets: []struct{ name, desc string }{
				{"test", "Run unit tests with race detection and coverage"},
				{"benchmark", "Run performance benchmarks"},
				{"coverage", "Generate and open HTML coverage report"},
				{"manualTest", "Run the manual test script"},
			},
		},
		{
			name: "Code Quality",
			targets: []struct{ name, desc string }{
				{"format", "Format all Go files"},
				{"formatChanged", "Format only changed Go files"},
				{"vet", "Run go vet"},
				{"lint", "Run all linters (vet + staticcheck + golangci-lint)"},
				{"lintStatic", "Run staticcheck"},
				{"lintGolangci", "Run golangci-lint"},
			},
		},
		{
			name: "Dependencies",
			targets: []struct{ name, desc string }{
				{"tidy", "Run go mod tidy"},
				{"modVerify", "Verify go.mod and go.sum integrity"},
				{"govulncheck", "Check for known vulnerabilities"},
			},
		},
		{
			name: "CI/CD & Automation",
			targets: []struct{ name, desc string }{
				{"ci", "Run full CI pipeline"},
				{"generateCheck", "Ensure generated files are up to date"},
				{"installHooks", "Install Git pre-commit hooks"},
			},
		},
	}

	for _, section := range sections {
		fmt.Printf("%s:\n", section.name)
		for _, target := range section.targets {
			fmt.Printf("  %-20s %s\n", target.name, target.desc)
		}
		fmt.Println()
	}

	fmt.Println("Tool Versions:")
	fmt.Printf("  staticcheck:    %s\n", toolVersionStaticcheck)
	fmt.Printf("  golangci-lint:  %s\n", toolVersionGolangciLint)
	fmt.Printf("  govulncheck:    %s\n", toolVersionGovulncheck)
	fmt.Printf("  goimports:      %s\n", toolVersionGoimports)
	fmt.Printf("  golines:        %s\n", toolVersionGolines)
	fmt.Printf("  gci:            %s\n", toolVersionGci)
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  mage run")
	fmt.Println("  ARGS='--verbose-errors' mage run")
	fmt.Println("  mage test")
	fmt.Println("  COVERPROFILE=coverage.out mage test")
}
