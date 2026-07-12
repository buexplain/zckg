package main

import (
	"os"

	"github.com/buexplain/zckg/zc/model"
	"github.com/buexplain/zckg/zc/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "zc",
	Short: "zc 命令行工具",
	Long:  "zc 是一个基于 Cobra 的命令行工具，支持开发多个子命令。",
}

func init() {
	rootCmd.AddCommand(version.Cmd)
	rootCmd.AddCommand(model.Cmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
