package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

var longDesc = "\"Sentinel is a cloud-native CLI tool designed for secure and reliable database backup and restoration," +
	" supporting MySQL, MariaDB, PostgreSQL, and MongoDB. With advanced features like AES-256 encryption for data " +
	"security, scheduled backups, and real-time notifications, Sentinel ensures your backups are protected and " +
	"accessible. Store backups on popular cloud services such as AWS S3, Google Drive, or MinIO, and easily monitor" +
	" operations with success or failure notifications. Sentinel is built to simplify database management, " +
	"allowing users to automate, secure, and manage their backup workflows efficiently.\""

var RootCmd = &cobra.Command{
	Use:   "sentinel",
	Short: "Open-source tool for automated backup and restoration supporting SQL and NoSQL databases",
	Long:  longDesc,
}

func Execute() {
	err := RootCmd.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
