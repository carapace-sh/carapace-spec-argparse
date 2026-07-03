package cmd

import (
	"fmt"
	"os"
	"path"

	"github.com/carapace-sh/carapace"
	"github.com/carapace-sh/carapace-spec-argparse/pkg/argparse"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var rootCmd = &cobra.Command{
	Use:   "carapace-spec-argparse",
	Short: "argparse spec scraper for carapace",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}

		spec, err := argparse.ParseJSON(content)
		if err != nil {
			return err
		}

		noDoc := cmd.Flag("no-doc").Changed
		command := toSpecCommand(spec, noDoc)

		if cmd.Flag("stdout").Changed {
			m, err := yaml.Marshal(command)
			if err != nil {
				return err
			}
			fmt.Println("# yaml-language-server: $schema=https://carapace.sh/schemas/command.json")
			fmt.Print(string(m))
			return nil
		}

		dir := cmd.Flag("target").Value.String()
		if dir == "" {
			dir, err = os.MkdirTemp("", "carapace-spec-argparse-*")
			if err != nil {
				return err
			}
		} else if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}

		cliName := spec.Cli.Name
		if cliName == "" {
			cliName = "argparse"
		}

		for _, subCommand := range command.Commands {
			m, err := yaml.Marshal(subCommand)
			if err != nil {
				return err
			}
			m = append([]byte("# yaml-language-server: $schema=https://carapace.sh/schemas/command.json\n"), m...)
			outPath := path.Join(dir, fmt.Sprintf("%s.%s.yaml", cliName, subCommand.Name))
			fmt.Fprintln(os.Stderr, outPath)
			if err := os.WriteFile(outPath, m, 0o644); err != nil {
				return err
			}
		}

		command.Commands = nil
		m, err := yaml.Marshal(command)
		if err != nil {
			return err
		}
		m = append([]byte("# yaml-language-server: $schema=https://carapace.sh/schemas/command.json\n"), m...)
		outPath := path.Join(dir, fmt.Sprintf("%s.yaml", cliName))
		fmt.Fprintln(os.Stderr, outPath)
		if err := os.WriteFile(outPath, m, 0o644); err != nil {
			return err
		}
		return nil
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	carapace.Gen(rootCmd).Standalone()
	rootCmd.Flags().Bool("no-doc", false, "strip documentation")
	rootCmd.Flags().Bool("stdout", false, "print to stdout")
	rootCmd.Flags().String("target", "", "target directory")
	rootCmd.MarkFlagsMutuallyExclusive("stdout", "target")

	carapace.Gen(rootCmd).PositionalCompletion(
		carapace.ActionFiles(),
	)
}
