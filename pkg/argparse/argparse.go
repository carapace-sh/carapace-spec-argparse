// Package argparse provides reusable Go types for the carapace-spec-argparse
// JSON schema. The Python scraper (scrape.py) emits JSON matching these types;
// the ToSpecCommand method converts them into carapace-spec command.Command
// values that can be marshaled to YAML.
package argparse

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/carapace-sh/carapace-spec/pkg/command"
	"github.com/neurosnap/sentences"
	"github.com/neurosnap/sentences/english"
)

// Spec is the top-level JSON schema emitted by scrape.py.
type Spec struct {
	Cli      Cli                  `json:"cli"`
	Commands map[string]FlatCmd   `json:"commands"`
	Groups   map[string]GroupInfo `json:"groups"`
}

// Cli holds metadata about the scraped CLI.
type Cli struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// FlatCmd is a single command in the flat command dict (keyed by space-separated path).
type FlatCmd struct {
	Description string     `json:"description"`
	Arguments   []Argument `json:"arguments"`
	Group       string     `json:"group"`
}

// Argument is a single flag/argument in the JSON schema.
type Argument struct {
	Name     string   `json:"name"`
	Options  []string `json:"options"`
	Help     string   `json:"help"`
	Required bool     `json:"required"`
	Choices  []string `json:"choices"`
	Type     string   `json:"type"`
	Nargs    string   `json:"nargs"`
	Default  any      `json:"default"`
	Metavar  string   `json:"metavar"`
	IsBool   bool     `json:"is_bool"`
}

// GroupInfo describes a command group in the JSON schema.
type GroupInfo struct {
	Help   string               `json:"help"`
	Groups map[string]GroupInfo `json:"groups"`
}

var tokenizer *sentences.DefaultSentenceTokenizer

func init() {
	t, err := english.NewSentenceTokenizer(nil)
	if err != nil {
		panic(err.Error())
	}
	tokenizer = t
}

// firstSentence extracts the first sentence from a help string.
func firstSentence(s string) string {
	if s == "" {
		return ""
	}
	tokens := tokenizer.Tokenize(s)
	if len(tokens) > 0 {
		return strings.SplitN(tokens[0].Text, "\n", 2)[0]
	}
	return s
}

// ToFlag converts an Argument to a carapace-spec Flag.
func (a Argument) ToFlag() command.Flag {
	f := command.Flag{
		Description: firstSentence(a.Help),
		Value:       !a.IsBool,
		Required:    a.Required,
	}

	for _, opt := range a.Options {
		if long, ok := strings.CutPrefix(opt, "--"); ok {
			f.Longhand = long
		} else if short, ok := strings.CutPrefix(opt, "-"); ok && short != "" {
			f.Shorthand = short
		}
	}

	switch a.Nargs {
	case "*", "+", "-1":
		f.Nargs = -1
	}

	if a.Default != nil && !a.IsBool {
		f.Default = fmt.Sprint(a.Default)
	}

	return f
}

// ToSpecCommand converts the entire Spec into a carapace-spec Command tree.
// The root command contains global flags and all top-level subcommands.
func (s Spec) ToSpecCommand() command.Command {
	root := command.Command{
		Name: s.Cli.Name,
	}
	root.Completion.Flag = make(map[string][]string)
	root.Documentation.Flag = make(map[string]string)

	tree := buildCommandTree(s.Commands, s.Groups)

	for name, flat := range s.Commands {
		if name == "" || !strings.Contains(name, " ") {
			if name == s.Cli.Name || name == "" {
				addFlagsToCommand(&root, flat.Arguments)
			}
		}
	}

	for _, child := range tree {
		root.Commands = append(root.Commands, child)
	}
	slices.SortFunc(root.Commands, func(a, b command.Command) int {
		return strings.Compare(a.Name, b.Name)
	})

	return root
}

// commandNode is an intermediate structure used while building the tree.
type commandNode struct {
	name        string
	description string
	flags       []Argument
	children    map[string]*commandNode
	childOrder  []string
}

// buildCommandTree constructs a nested command.Command tree from the flat
// command dict. Keys are split on spaces to determine depth.
func buildCommandTree(commands map[string]FlatCmd, groups map[string]GroupInfo) []command.Command {
	root := &commandNode{children: make(map[string]*commandNode)}

	for groupPath, info := range groups {
		parts := strings.Split(groupPath, " ")
		node := root
		for _, part := range parts {
			child, exists := node.children[part]
			if !exists {
				child = &commandNode{name: part, children: make(map[string]*commandNode)}
				node.children[part] = child
				node.childOrder = append(node.childOrder, part)
			}
			node = child
		}
		if node.description == "" {
			node.description = info.Help
		}
	}

	for name, flat := range commands {
		parts := strings.Split(name, " ")
		node := root
		for _, part := range parts {
			child, exists := node.children[part]
			if !exists {
				child = &commandNode{name: part, children: make(map[string]*commandNode)}
				node.children[part] = child
				node.childOrder = append(node.childOrder, part)
			}
			node = child
		}
		node.description = flat.Description
		node.flags = flat.Arguments
	}

	return convertNodes(root)
}

// convertNodes recursively converts commandNode children to command.Command.
func convertNodes(node *commandNode) []command.Command {
	result := make([]command.Command, 0, len(node.children))
	for _, name := range node.childOrder {
		child := node.children[name]
		cmd := command.Command{
			Name:        child.name,
			Description: firstSentence(child.description),
		}
		cmd.Completion.Flag = make(map[string][]string)
		cmd.Documentation.Flag = make(map[string]string)
		addFlagsToCommand(&cmd, child.flags)

		if len(child.children) > 0 {
			cmd.Commands = convertNodes(child)
			slices.SortFunc(cmd.Commands, func(a, b command.Command) int {
				return strings.Compare(a.Name, b.Name)
			})
		}
		result = append(result, cmd)
	}
	return result
}

// addFlagsToCommand adds arguments as flags to a spec command, including
// completion values for choices and documentation.
func addFlagsToCommand(cmd *command.Command, args []Argument) {
	for _, arg := range args {
		if len(arg.Options) == 0 {
			continue
		}
		f := arg.ToFlag()
		cmd.AddFlag(f)

		flagName := f.Name()
		if len(arg.Choices) > 0 {
			cmd.Completion.Flag[flagName] = arg.Choices
		}
		if arg.Help != "" {
			cmd.Documentation.Flag[flagName] = arg.Help
		}
	}
}

// ParseJSON parses scrape.py JSON output and returns a Spec.
func ParseJSON(data []byte) (Spec, error) {
	var s Spec
	if err := json.Unmarshal(data, &s); err != nil {
		return Spec{}, err
	}
	return s, nil
}

// Command is a convenience wrapper that parses JSON and returns a spec Command.
func Command(data []byte) (command.Command, error) {
	s, err := ParseJSON(data)
	if err != nil {
		return command.Command{}, err
	}
	return s.ToSpecCommand(), nil
}
