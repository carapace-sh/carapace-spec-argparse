# AGENTS.md

## Architecture

Two-component design mirroring the `carapace-gcloud` pattern:

1. **Python scraper** (`scrape.py`) — runs inside the target CLI's environment, walks the argparse parser tree, outputs JSON to stdout
2. **Go spec converter** (`cmd/`) — reads JSON, converts to carapace-spec YAML

### Why two languages?

- **Python** is required to introspect argparse parsers — the parser objects are live Python objects with private attributes (`_actions`, `_subparsers`) that can't be accessed from Go.
- **Go** is used for the spec conversion to reuse `carapace-spec/pkg/command` types and YAML marshaling, consistent with all other carapace-spec scrapers.

### Data Flow

```
Target CLI (Python, in Docker or local)
  → scrape.py (walks argparse parser tree)
    → JSON (command structure with all metadata)
      → carapace-spec-argparse (Go binary, reads JSON)
        → carapace-spec YAML (one file per top-level group)
```

## Components

### scrape.py

Python introspection script with pluggable scraping methods:

- `--method argparse`: walks `parser._actions` and `_SubParsersAction` recursively. Works with any argparse CLI. Requires `--import "module:function"` to locate the parser factory.
- `--method knack`: uses Knack's `command_table` API for richer metadata. Works with Azure CLI (`az`), Service Fabric CLI (`sfctl`), and other Knack-based CLIs.
- `--method auto` (default): detects knack if available, falls back to argparse.

Outputs JSON to stdout matching the schema defined in `pkg/argparse/argparse.go`.

### pkg/argparse/argparse.go

Reusable Go types matching the JSON schema:

- `Spec` — top-level: `Cli`, `Commands` (flat dict), `Groups`
- `FlatCmd` — single command with `Description`, `Arguments`, `Group`
- `Argument` — flag metadata: `Options`, `Help`, `Required`, `Choices`, `Type`, `Nargs`, `IsBool`
- `GroupInfo` — group with `Help` and nested `Groups`

`Spec.ToSpecCommand()` converts the flat command dict into a nested `command.Command` tree by splitting keys on spaces. Uses the `sentences` tokenizer for first-sentence extraction from help text.

### cmd/

- `root.go` — cobra CLI with `--target`, `--stdout`, `--no-doc` flags. Reads JSON file, converts to spec, writes one YAML per top-level group + root file.
- `command.go` — `toSpecCommand()` wrapper and `stripDoc()` helper.
- `carapace-spec-argparse/main.go` — entry point.

## Field Mapping

### argparse → JSON

| argparse attribute | JSON field | Notes |
|-------------------|-----------|-------|
| `action.option_strings` | `options` | `['--name', '-n']` |
| `action.dest` | `name` | Internal variable name |
| `action.help` | `help` | Help text |
| `action.required` | `required` | bool |
| `action.choices` | `choices` | List or null |
| `action.type` | `type` | Stringified type callable |
| `action.nargs` | `nargs` | `'?', '*', '+', int, null` |
| `action.default` | `default` | Default value |
| `action.metavar` | `metavar` | Display name |
| store_true/store_false | `is_bool` | Whether flag takes no value |

### JSON → carapace-spec

| JSON field | spec.Command field | Notes |
|-----------|-------------------|-------|
| `options[0]` | `Flag.Longhand` | First long option (strip `--`) |
| `options[1]` | `Flag.Shorthand` | Short option if exists (strip `-`) |
| `help` | `Flag.Description` | First sentence via tokenizer |
| `required` | `Flag.Required` | bool |
| `is_bool != true` | `Flag.Value` | Takes argument if not bool |
| `choices` | `Completion.Flag[name]` | Static completion values |
| `nargs` (`*`, `+`, `-1`) | `Flag.Nargs = -1` | Variadic |
| `default` | `Flag.Default` | Stringified; bool flags excluded (common `False` default would add noise) |

## Build & Test

```bash
go build ./...
go vet ./...
gofmt -d -s .
```

Test the full pipeline:

```bash
# Create a test argparse CLI
echo 'import argparse
def get_parser():
    p = argparse.ArgumentParser(prog="testcli")
    sub = p.add_subparsers(dest="cmd")
    vm = sub.add_parser("vm", help="Manage VMs.")
    vm_sub = vm.add_subparsers(dest="vm_cmd")
    create = vm_sub.add_parser("create", help="Create a VM.")
    create.add_argument("--name", "-n", required=True, help="VM name.")
    return p
' > /tmp/testcli.py

# Scrape
PYTHONPATH=/tmp python scrape.py --method argparse --cli testcli --import "testcli:get_parser" > /tmp/testcli.json

# Convert
go run ./cmd/carapace-spec-argparse --stdout /tmp/testcli.json
```

## Key Design Decisions

1. **JSON intermediate format** — decouples Python scraping from Go spec generation. The JSON schema is stable and can be produced by any scraping method.
2. **Pluggable scrapers** — different Python CLIs have different initialization patterns. The `--method` flag lets users choose the right approach.
3. **Flat command dict with space-separated keys** — matches Knack's `command_table` pattern and simplifies the JSON schema. The Go converter builds the tree by splitting on spaces.
4. **No runtime registration** — unlike kingpin/urfavecli scrapers, this is a standalone tool. Python CLIs can't easily have Go code injected into them, so the scrape-and-convert pattern is necessary.
