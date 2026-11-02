// Command crosswire is the single binary: the workbench server and the
// reference targets.
//
//	crosswire serve  [--listen 127.0.0.1:7117] [--data ~/.crosswire]
//	crosswire target <home-agent|voice-agent|camera-agent|expense-agent|inbox-agent|desk-agent> [flags]
//	crosswire bridge [--simulate | --serial-port PORT | --exec HELPER] [flags]
//	crosswire selftest [--repeats N]
//	crosswire version
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/bridge"
	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/selftest"
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
	case "bridge":
		err = bridge.Main(args)
	case "selftest":
		err = runSelftest(args)
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

// runSelftest starts the in-process fakes, runs one trial set through every
// connector and prints a table. It exits non-zero when any row fails.
func runSelftest(args []string) error {
	fs := flag.NewFlagSet("selftest", flag.ContinueOnError)
	repeats := fs.Int("repeats", 5, "trials per arm for each connector")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if _, ok := selftest.Run(ctx, os.Stdout, *repeats); !ok {
		return fmt.Errorf("selftest failed")
	}
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  crosswire serve  [--listen 127.0.0.1:7117] [--data DIR]   start the workbench (default)
  crosswire target NAME [--port N] [--policy vulnerable|hardened] [--guardrail[=chat|perception|both]]
                       [--no-quarantine] [--no-withhold] [--no-confirm] [--no-interlock] [--temperature T]
                       [--base-url URL] [--api-key-env VAR] [--sip-listen HOST:PORT]
                                                          run a reference target
  crosswire bridge [--kind led|gpio|ble|nfc] [--device-id ID] [--resource R] [--listen 127.0.0.1:9910]
                   [--simulate | --serial-port PORT | --exec HELPER]
                                                          run the device bridge a bridge target talks to
  crosswire selftest [--repeats N]                        run one trial set through every connector against in-process fakes
  crosswire version`)
}
