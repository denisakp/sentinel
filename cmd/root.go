package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

var RootCmd = &cobra.Command{
	Use:   "sentinel",
	Short: "Open-source tool backs up SQL/NoSQL dbs, supports multiple storage options. Simplifies automation.",
	Long:  `Sentinel is an open-source, cloud-native tool designed for backing up and restoring SQL and NoSQL databases, including PostgresSQL, MySQL, MariaDB, and MongoDB. It supports local and cloud storage options like Amazon S3, Google Drive, and Dropbox, providing data security through encryption and backup integrity validation. With built-in notifications, scheduling (cron jobs), and retention policies, Sentinel simplifies and automates database management in Docker, Kubernetes, and local environments.`,
}

func Execute() {
	err := RootCmd.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
