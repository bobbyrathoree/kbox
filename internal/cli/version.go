package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print kbox version information",
	Run: func(cmd *cobra.Command, args []string) {
		short, _ := cmd.Flags().GetBool("short")
		outputFormat := GetOutputFormat(cmd)

		// JSON output
		if outputFormat == "json" {
			json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"version":   Version,
				"gitCommit": GitCommit,
				"buildDate": BuildDate,
				"goVersion": runtime.Version(),
				"platform":  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
			})
			return
		}

		// Short text output
		if short {
			fmt.Println(Version)
			return
		}

		// Full text output
		fmt.Printf("kbox version %s\n", Version)
		fmt.Printf("  git commit: %s\n", GitCommit)
		fmt.Printf("  build date: %s\n", BuildDate)
		fmt.Printf("  go version: %s\n", runtime.Version())
		fmt.Printf("  platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	versionCmd.Flags().Bool("short", false, "Print just the version number")
	rootCmd.AddCommand(versionCmd)
}
