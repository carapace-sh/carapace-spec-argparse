# carapace-spec-argparse

[Spec](https://github.com/carapace-sh/carapace-spec) generation for Python [argparse](https://docs.python.org/3/library/argparse.html) CLIs.

A two-component tool that scrapes any Python CLI using `argparse` (or [Knack](https://github.com/microsoft/knack)-based CLIs like Azure CLI) and generates carapace-spec YAML for [carapace](https://carapace.sh) shell completion.

## How it works

```
Target CLI (Python)
  → scrape.py (walks argparse parser tree)
    → JSON (command structure with all metadata)
      → carapace-spec-argparse (Go binary, reads JSON)
        → carapace-spec YAML (one file per top-level group)
```

1. **`scrape.py`** runs inside the target CLI's Python environment and introspects the argparse parser tree, outputting JSON to stdout.
2. **`carapace-spec-argparse`** (Go binary) reads the JSON and converts it to carapace-spec YAML files.

## Usage

### Generic argparse CLI

```bash
# Scrape a CLI that exposes a parser factory
python scrape.py --method argparse --cli mycli --import "mycli.parser:get_parser" > mycli.json

# Generate YAML specs to a directory
carapace-spec-argparse --target specs/ mycli.json

# Or print to stdout
carapace-spec-argparse --stdout mycli.json
```

### Knack-based CLI (e.g. Azure CLI)

```dockerfile
FROM mcr.microsoft.com/azure-cli:2.71.0
ADD scrape.py /
CMD ["python", "/scrape.py", "--method", "knack", "--cli", "az"]
```

```bash
# Scrape inside Docker
docker compose run --rm az > az_commands.json

# Generate specs
carapace-spec-argparse --target cmd/carapace-az/cmd/azcli az_commands.json
```

### Flags

| Flag | Description |
|------|-------------|
| `--target <dir>` | Write YAML files to the given directory (one per top-level group + root) |
| `--stdout` | Print the full spec to stdout instead of writing files |
| `--no-doc` | Strip documentation sections from the output |

`--target` and `--stdout` are mutually exclusive.

### Scraping methods

| Method | Description |
|--------|-------------|
| `auto` | Auto-detect based on available imports (default) |
| `argparse` | Raw argparse introspection via `parser._actions` (generic, works with any argparse CLI) |
| `knack` | Knack `command_table` API (Azure CLI, Service Fabric CLI) |

## JSON Schema

The Python scraper outputs a standardized JSON schema:

```json
{
  "cli": { "name": "az", "version": "2.71.0" },
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
          "metavar": null,
          "is_bool": false
        }
      ],
      "group": "vm"
    }
  },
  "groups": {
    "vm": { "help": "Manage virtual machines.", "groups": {} }
  }
}
```

Commands use flat space-separated keys (e.g. `"vm create"`). The Go converter builds the nested command tree by splitting on spaces.

## Project Structure

```
carapace-spec-argparse/
├── scrape.py                    # Python introspection script
├── cmd/
│   ├── command.go               # Spec conversion helpers
│   ├── root.go                  # CLI: reads JSON, writes YAML
│   └── carapace-spec-argparse/
│       └── main.go              # Entry point
└── pkg/
    └── argparse/
        └── argparse.go          # Reusable Go types + ToSpecCommand()
```

The `pkg/argparse` package is importable by other projects (e.g. `carapace-az`) that need to convert the JSON schema to carapace-spec commands.

## Reusable Package

```go
import (
    "github.com/carapace-sh/carapace-spec-argparse/pkg/argparse"
)

spec, err := argparse.ParseJSON(jsonData)
cmd := spec.ToSpecCommand()
```
