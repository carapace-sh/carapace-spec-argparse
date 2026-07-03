package cmd

import (
	"github.com/carapace-sh/carapace-spec-argparse/pkg/argparse"
	"github.com/carapace-sh/carapace-spec/pkg/command"
)

// toSpecCommand converts the argparse Spec to a carapace-spec Command,
// optionally stripping documentation.
func toSpecCommand(spec argparse.Spec, noDoc bool) command.Command {
	cmd := spec.ToSpecCommand()
	if noDoc {
		stripDoc(&cmd)
	}
	return cmd
}

func stripDoc(cmd *command.Command) {
	cmd.Documentation.Command = ""
	cmd.Documentation.Flag = nil
	for i := range cmd.Commands {
		stripDoc(&cmd.Commands[i])
	}
}
