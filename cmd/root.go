package cmd

import (
	"os"
	"path"
	"strings"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   conf.APP_NAME,
	Short: conf.APP_DESC,
}

func Execute() {
	if shouldRunDesktopByDefault(os.Args) {
		rootCmd.SetArgs([]string{"desktop"})
	}
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func shouldRunDesktopByDefault(args []string) bool {
	if len(args) != 1 {
		return false
	}
	executable := strings.ToLower(path.Base(strings.ReplaceAll(args[0], `\`, "/")))
	return strings.HasPrefix(executable, "octopus-desktop")
}
