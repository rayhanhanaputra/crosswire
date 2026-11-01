// Command crosswire is the single binary: the workbench server and the
// reference targets.
//
//	crosswire serve  [--listen 127.0.0.1:7117] [--data ~/.crosswire]
//	crosswire target <home-agent|voice-agent|camera-agent|expense-agent|inbox-agent|desk-agent> [flags]
//	crosswire version
package main

import (
	"fmt"
	"os"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/server"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/targets"
)

// Version is set at build time with -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = server.Main(Version, args)
	case "target":
		err = targets.Main(Version, args)
	case "version", "-v", "--version":
		fmt.Println("crosswire", Version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "crosswire:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  crosswire serve  [--listen 127.0.0.1:7117] [--data DIR]   start the workbench (default)
  crosswire target NAME [--port N] [--policy vulnerable|hardened] [--guardrail[=chat|perception|both]]
                       [--no-quarantine] [--no-withhold] [--no-confirm] [--no-interlock] [--temperature T]
                       [--base-url URL] [--api-key-env VAR] [--sip-listen HOST:PORT]
                                                          run a reference target
  crosswire version`)
}
