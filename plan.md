# Implementation Plan: carapace-spec-argparse

A generic Python argparse spec scraper for [carapace](https://carapace.sh) — generates carapace-spec YAML from any Python CLI that uses `argparse` with subparsers.

## Motivation

Many Python CLIs use `argparse` with hierarchical subparsers (`add_subparsers`). There is currently no carapace scraper for the Python/argparse ecosystem:

| Existing scrapers | Language | Framework |
|------------------|----------|-----------|
| `carapace-spec-kingpin` | Go | kingpin |
| `carapace-spec-clap` | Rust | clap |
| `carapace-spec-urfavecli` | Go | urfave/cli |
| `carapace-spec-kong` | Go | kong |
| **carapace-spec-argparse** | **Python** | **argparse** (new) |

CLIs that could benefit:

| CLI | Framework | Notes |
|-----|-----------|-------|
| Azure CLI (`az`) | Knack (extends argparse) | Primary use case — `carapace-az` depends on this |
| Service Fabric CLI (`sfctl`) | Knack | Same `command_table` API |
| Conda (`conda`) | Custom argparse subclasses | Hierarchical subparsers |
| pip | argparse | Subcommands: install, list, freeze, etc. |
| Any custom argparse CLI | argparse | Generic support |

## Architecture

Two-component design:

1. **Python introspection script** (`scrape.py`) — runs inside the target CLI's environment, walks the argparse parser tree, outputs JSON
2. **Go spec converter** (`cmd/`) — reads JSON, converts to carapace-spec YAML

This mirrors the `carapace-gcloud` pattern (Python/CLI produces JSON → Go converts to YAML) rather than the Go-native scraper pattern (kingpin/urfavecli where the scraper runs in-process).

### Why two languages?

- **Python** is required to introspect argparse parsers — the parser objects are live Python objects with private attributes (`_actions`, `_subparsers`) that can't be accessed from Go
- **Go** is used for the spec conversion to reuse `carapace-spec/pkg/command` types and YAML marshaling, consistent with all other carapace-spec scrapers

### Data Flow

```
Target CLI (Python, in Docker or local)
  → scrape.py (walks argparse parser tree)
    → JSON (command structure with all metadata)
      → carapace-spec-argparse (Go binary, reads JSON)
        → carapace-spec YAML (one file per top-level group)
```

## Scraping Approaches

### Approach A: Raw argparse introspection (generic)

Walks the argparse parser tree using `parser._actions` and `_SubParsersAction`:

```python
def walk_parser(parser):
    commands = {}
    flags = {}

    for action in parser._actions:
        if isinstance(action, argparse._SubParsersAction):
            for name in action._choices_actions:
                subparser = action.choices[name.dest]
                commands[name.dest] = walk_parser(subparser)
                commands[name.dest]["help"] = name.help
        elif action.option_strings:
            flags[action.dest] = {
                "options": action.option_strings,   # ['--name', '-n']
                "help": action.help,
                "required": action.required,
                "choices": list(action.choices) if action.choices else None,
                "type": str(action.type) if action.type else None,
                "nargs": action.nargs,
                "default": action.default,
                "metavar": action.metavar,
            }

    return {"flags": flags, "commands": commands}
```

**Pros**: Works with any argparse CLI — no framework dependency
**Cons**: Uses private argparse attributes (`_actions`, `_choices_actions`), fragile across Python versions. Each CLI initializes its parser differently (no standard entry point).

### Approach B: Knack command_table introspection (framework-specific)

Uses Knack's public `command_table` API (available in az CLI, sfctl):

```python
from azure.cli.core.file_util import create_invoker_and_load_cmds_and_args
create_invoker_and_load_cmds_and_args(cli)

for name, cmd in cli.invocation.commands_loader.command_table.items():
    # cmd.name, cmd.description, cmd.arguments (rich metadata)
```

**Pros**: Public API, richer metadata (completers, validators, deprecation, config defaults)
**Cons**: Only works for Knack-based CLIs

### Approach C: Pluggable scrapers (chosen)

Support both approaches via a pluggable scraper system:

```
scrape.py
  ├── --method argparse   # raw argparse introspection (generic)
  ├── --method knack      # knack command_table (az, sfctl)
  └── --method click      # click to_info_dict() (future)
```

The default method auto-detects based on available imports. For `carapace-az`, the Dockerfile will use `--method knack` for richer metadata. For other CLIs, `--method argparse` provides generic support.

## Project Structure

```
carapace-spec-argparse/
├── plan.md                      # This file
├── go.mod
├── go.sum
├── LICENSE
├── README.md
├── AGENTS.md
├── .gitignore
├── scrape.py                    # Python introspection script (generic)
├── cmd/
│   ├── root.go                  # Reads JSON, converts to YAML specs
│   ├── command.go               # JSON struct definitions + ToSpecCommand()
│   └── main.go                  # Entry point
└── pkg/
    └── argparse/
        └── argparse.go          # Reusable Go types for argparse JSON schema
```

## JSON Schema

The Python scraper outputs a standardized JSON schema regardless of the scraping method used:

```json
{
  "cli": {
    "name": "az",
    "version": "2.71.0"
  },
  "commands": {
    "vm create": {
      "description": "Create a virtual machine.",
      "arguments": [
        {
          "name": "resource_group_name",
          "options": ["--resource-group", "-g"],
          "help": "Name of resource group.",
          "required": true,
          "choices": null,
          "type": "str",
          "nargs": null,
          "default": null,
          "metavar": null
        }
      ],
      "group": "vm"
    },
    "network vnet create": {
      "description": "Create a virtual network.",
      "arguments": [...],
      "group": "network"
    }
  },
  "groups": {
    "vm": {
      "help": "Manage virtual machines.",
      "groups": {
        "vm image": { "help": "..." }
      }
    },
    "network": {
      "help": "Manage Azure Network resources.",
      "groups": {
        "network vnet": { "help": "..." },
        "network vnet subnet": { "help": "..." }
      }
    }
  }
}
```

### Field Mapping: argparse → JSON

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

### Field Mapping: JSON → carapace-spec

| JSON field | spec.Command field | Notes |
|-----------|-------------------|-------|
| `options[0]` | `Flag.Longhand` | First long option (strip `--`) |
| `options[1]` | `Flag.Shorthand` | Short option if exists (strip `-`) |
| `help` | `Flag.Description` | First sentence via tokenizer |
| `required` | `Flag.Required` | bool |
| `type != bool` | `Flag.Value` | Takes argument if not bool |
| `choices` | `Completion.Flag[name]` | Static completion values |
| `nargs` (`*`, `+`, `-1`) | `Flag.Nargs = -1` | Variadic |

## Implementation Steps

### Phase 1: Python Scraper

1. **Write `scrape.py`**
   - `--method auto` (default): detect knack vs raw argparse
   - `--method knack`: use `create_invoker_and_load_cmds_and_args()` + `command_table`
   - `--method argparse`: walk `parser._actions` recursively
   - `--cli <name>`: CLI name for the JSON output
   - `--import <module>`: Python module to import that exposes the parser (for generic argparse mode)
   - Output JSON to stdout
   - Handle: nested subparsers, aliases, choices, required flags, types, nargs, help text
   - Filter out `--help`/`-h` (carapace adds its own)

2. **Test with az CLI**
   - Run inside `mcr.microsoft.com/azure-cli` Docker container
   - Verify JSON output covers all command groups

### Phase 2: Go Spec Converter

3. **Write `cmd/command.go`**
   - Go structs matching the JSON schema: `Cli`, `Command`, `Argument`, `Group`
   - `ToSpecCommand()` — converts to `carapace-spec/pkg/command.Command`
   - Use `sentences` tokenizer for first-sentence extraction
   - Group commands by top-level group name
   - Build nested command tree from flat `commands` dict (split on spaces)

4. **Write `cmd/root.go`**
   - Accept JSON file path as positional argument
   - Flags: `--target`, `--stdout`, `--no-doc` (mutually exclusive)
   - Parse JSON → build command tree → write one YAML per top-level group
   - Write root spec with global flags

5. **Write `cmd/main.go`**
   - Entry point calling `cmd.Execute()`

### Phase 3: Reusable Package

6. **Write `pkg/argparse/argparse.go`**
   - Export the JSON types and `ToSpecCommand()` for reuse by other projects
   - `carapace-az` will import this package directly

### Phase 4: Testing & Documentation

7. **Write `README.md`** with usage examples
8. **Write `AGENTS.md`** with architecture docs
9. **Test with multiple CLIs** (az, conda, pip if possible)

## Usage Examples

### With Azure CLI (knack method)

```dockerfile
# In a Dockerfile for carapace-az
FROM mcr.microsoft.com/azure-cli:2.71.0
ADD scrape.py /
CMD ["python", "/scrape.py", "--method", "knack", "--cli", "az"]
```

```bash
# Generate specs
docker compose run --rm az > az_commands.json
carapace-spec-argparse --target cmd/carapace-az/cmd/azcli --no-doc az_commands.json
```

### With any argparse CLI (generic method)

```bash
# Scrape a generic argparse CLI
python scrape.py --method argparse --cli "mycli" --import "mycli.parser:get_parser" > mycli.json

# Generate specs
carapace-spec-argparse --target specs/ mycli.json
```

## Key Design Decisions

1. **JSON intermediate format**: Decouples Python scraping from Go spec generation. The JSON schema is stable and can be produced by any scraping method. This allows swapping the Python script without changing the Go converter.

2. **Pluggable scrapers**: Different Python CLIs have different initialization patterns. The `--method` flag lets users choose the right approach without modifying the tool.

3. **Flat command dict with space-separated keys**: Matches knack's `command_table` pattern and simplifies the JSON schema. The Go converter builds the tree by splitting on spaces.

4. **No runtime registration**: Unlike kingpin/urfavecli scrapers (which register a `_carapace spec` subcommand at runtime), this is a standalone tool. Python CLIs can't easily have Go code injected into them, so the scrape-and-convert pattern is necessary.

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `carapace-spec` | `command.Command` type, YAML marshaling |
| `cobra` | CLI framework for the converter binary |
| `carapace` | Completion for the converter itself |
| `yaml.v3` | YAML output |
| `sentences` | Sentence tokenizer for descriptions |

## Open Questions

1. **Click support**: Should we add a `--method click` that uses Click's `to_info_dict()`? Click 8.0+ has clean introspection. Could be a future addition.
2. **argcomplete integration**: Should the scraper also detect argcomplete completers registered on arguments? This could add dynamic completion hints to the spec.
3. **Extension/plugin support**: For CLIs that load plugins dynamically (e.g., az extensions, conda plugins), should the scraper attempt to load those too?
