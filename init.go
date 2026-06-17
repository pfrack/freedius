package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "embed"

	"github.com/pfrack/freedius/internal/envinject"
)

//go:embed templates/starter.yaml
var starterTemplate string

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	flagOutput := fs.String("output", "freedius.yaml", "output path for the config file")
	flagForce := fs.Bool("force", false, "overwrite existing file (backup first)")
	flagDryRun := fs.Bool("dry-run", false, "print template to stdout without writing")
	flagNoEnv := fs.Bool("no-env", false, "skip writing ~/.claude/settings.json")
	flagShellInstall := fs.Bool("shell-install", false, "append env block to shell rc file")
	flagHost := fs.String("host", defaultHost, "host for injected env vars")
	flagPort := fs.Int("port", defaultPort, "port for injected env vars")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: freedius init [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	output := *flagOutput
	var backup string
	if *flagDryRun {
		fmt.Println(starterTemplate)
		return 0
	}

	if _, err := os.Stat(output); err == nil && !*flagForce {
		fmt.Fprintf(os.Stderr, "freedius: %s already exists (use --force to overwrite)\n", output)
		return 1
	}

	if *flagForce {
		if _, err := os.Stat(output); err == nil {
			backup = output + ".bak"
			if err := os.Rename(output, backup); err != nil {
				fmt.Fprintf(os.Stderr, "freedius: backup %s failed: %v\n", backup, err)
				return 1
			}
		}
	}

	if parent := filepath.Dir(output); parent != "." {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "freedius: cannot create directory %s: %v\n", parent, err)
			return 1
		}
	}

	if err := os.WriteFile(output, []byte(starterTemplate), 0o644); err != nil {
		if output != "" && backup != "" {
			if rerr := os.Rename(backup, output); rerr != nil {
				fmt.Fprintf(
					os.Stderr,
					"freedius: write %s failed (%v); backup restored from %s (recovery also failed: %v)\n",
					output,
					err,
					backup,
					rerr,
				)
			} else {
				fmt.Fprintf(os.Stderr, "freedius: write %s failed (%v); original restored from %s\n", output, err, backup)
			}
		}
		return 1
	}
	fmt.Printf("wrote %s\n", output)

	host := *flagHost
	port := *flagPort

	if !*flagNoEnv {
		if err := envinject.WriteSettingsJSON("", host, port, false); err != nil {
			fmt.Fprintf(os.Stderr, "freedius: %v\n", err)
		} else {
			fmt.Println("wrote ~/.claude/settings.json")
		}
	}

	if *flagShellInstall {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "freedius: cannot determine home directory: %v\n", err)
			return 1
		}
		shell := os.Getenv("SHELL")
		rcPath, err := envinject.WriteShellRC(home, shell, host, port, *flagForce, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "freedius: %v\n", err)
			return 1
		}
		fmt.Printf("wrote %s\n", rcPath)
	}

	return 0
}
