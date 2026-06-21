package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/pfrack/freedius/proxy/tui"
)

func handleAttach(args []string) int {
	return runAttach(args)
}

func runAttach(_ []string) int {
	sock := socketPath()

	client, err := NewIPCClient(sock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	defer func() { _ = client.Close() }()

	model := tui.NewAttachDashboard(
		client.Events(),
		client.Logs(),
		"", // cfgPath: not available in attach mode
		"127.0.0.1",
		0, // port: not available in attach mode
	)
	prog := tea.NewProgram(model)
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "freedius: attach error: %v\n", err)
		return 1
	}
	return 0
}
