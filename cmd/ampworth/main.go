// Command ampworth runs Ampworth's UK home energy calculators and reads its
// guides from a terminal, and serves them to AI assistants as a local MCP
// server. It's a thin client for the public API at ampworth.com: the
// calculations run there.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/ampworth/cli/internal/api"
)

// version is set at build time by the release tooling.
var version = "dev"

const usage = `ampworth: UK solar PV and heat pump calculators that follow the published
MCS standards and show their working, and the guides that explain them.

Usage:
  ampworth list                         list the calculators
  ampworth describe <calculator>        the fields a calculator takes
  ampworth run <calculator> [--field value ...]
                                        run a calculator
  ampworth search <words>               search the guides, articles and questions
  ampworth docs [--kind guide|article|faq] [--category <key>]
                                        list the documents
  ampworth read <kind>/<slug>           read one document, as Markdown
  ampworth mcp                          serve every tool as a local MCP server (stdio)
  ampworth version                      print the version

Options:
  --json         print the API's JSON instead of text
  --api <url>    the API to use (default https://ampworth.com/api/v1, or AMPWORTH_API)

Examples:
  ampworth run heat-pump-size --location birmingham --annual-fuel-use 12000 --bedrooms 3
  ampworth run solar-pv-output --postcode "BS1 5TR" --capacity-kwp 4.4 --orientation 30 --inclination 35
  ampworth search "flow temperature"
  ampworth read guides/sizing-a-heat-pump

The answers cover UK homes only, and are for understanding a system or checking
a quotation: an installation still needs a survey and a design from a certified
installer. More at https://ampworth.com/developers
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ampworth:", err)
		var usageErr usageError
		if errors.As(err, &usageErr) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// usageError is a mistake in how the command was called.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg + "\nRun 'ampworth help' for usage." }

// options are the flags every command accepts.
type options struct {
	json bool
	api  string
}

// splitOptions takes the shared flags out of args, wherever they appear.
func splitOptions(args []string) (options, []string, error) {
	opts := options{api: os.Getenv("AMPWORTH_API")}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			opts.json = true
		case a == "--api":
			if i+1 >= len(args) {
				return opts, nil, usageError{"--api needs a URL"}
			}
			i++
			opts.api = args[i]
		case strings.HasPrefix(a, "--api="):
			opts.api = strings.TrimPrefix(a, "--api=")
		default:
			rest = append(rest, a)
		}
	}
	return opts, rest, nil
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	opts, args, err := splitOptions(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return nil
	}
	client := api.New(opts.api, "ampworth-cli/"+version)
	cmd, rest := args[0], args[1:]

	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	case "version", "--version":
		fmt.Fprintln(stdout, "ampworth", version)
		return nil
	case "list":
		return cmdList(ctx, client, opts, stdout)
	case "describe":
		if len(rest) != 1 {
			return usageError{"describe takes one calculator, such as heat-pump-size"}
		}
		return cmdDescribe(ctx, client, opts, rest[0], stdout)
	case "run":
		if len(rest) == 0 {
			return usageError{"run needs a calculator, such as heat-pump-size"}
		}
		return cmdRun(ctx, client, opts, rest[0], rest[1:], stdout)
	case "search":
		return cmdSearch(ctx, client, opts, rest, stdout)
	case "docs":
		return cmdDocs(ctx, client, opts, rest, stdout)
	case "read":
		if len(rest) != 1 {
			return usageError{"read takes one document, such as guides/sizing-a-heat-pump"}
		}
		return cmdRead(ctx, client, opts, rest[0], stdout)
	case "mcp":
		return serveMCP(ctx, client, stdin, stdout)
	}
	return usageError{fmt.Sprintf("unknown command %q", cmd)}
}
