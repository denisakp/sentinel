package cmd

import (
	"github.com/denisakp/sentinel/pkg/backup"
	"github.com/denisakp/sentinel/pkg/backup/mysql_dump"
	"github.com/denisakp/sentinel/pkg/backup/pg_dump"
	"github.com/spf13/cobra"
	"os"
)

var dbType, host, port, user, password, database, output, additionalArgs, pgOutFormat, pgCompressionAlgo string
var compress bool
var pgCompressionLevel int
var err error

var BackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup your database",
	Long:  "Backup your database with the required options depending on the database type",
	Run: func(cmd *cobra.Command, args []string) {
		dbType, _ = cmd.Flags().GetString("type")
		if err = backup.ValidateDbType(dbType); err != nil {
			cmd.PrintErrln(err)
			return
		}

		host, _ = cmd.Flags().GetString("host")
		port, _ = cmd.Flags().GetString("port")
		user, _ = cmd.Flags().GetString("user")
		password, _ = cmd.Flags().GetString("password")
		database, _ = cmd.Flags().GetString("database")
		output, _ = cmd.Flags().GetString("output")
		additionalArgs, _ = cmd.Flags().GetString("args")

		if dbType == "postgres" {
			compress, _ = cmd.Flags().GetBool("compress")
			pgOutFormat, _ = cmd.Flags().GetString("pg-out-format")
			pgCompressionAlgo, _ = cmd.Flags().GetString("pg-compression-algo")
			pgCompressionLevel, _ = cmd.Flags().GetInt("pg-compression-level")

			if compress && pgOutFormat == "t" {
				cmd.PrintErrln("tar format does not support compression")
				os.Exit(1)
			}

			pda := &pg_dump.PgDumpArgs{
				Host:                 host,
				Port:                 port,
				Username:             user,
				Password:             password,
				Database:             database,
				OutName:              output,
				OutFormat:            pgOutFormat,
				Compress:             compress,
				CompressionAlgorithm: pgCompressionAlgo,
				CompressionLevel:     pgCompressionLevel,
				AdditionalArgs:       additionalArgs,
			}

			err = pg_dump.Backup(pda)
			if err != nil {
				cmd.PrintErrln(err)
				os.Exit(1)
			}
		}

		if dbType == "mysql" {
			mda := &mysql_dump.MySqlDumpArgs{
				Host:           host,
				Port:           port,
				Username:       user,
				Password:       password,
				Database:       database,
				OutName:        output,
				AdditionalArgs: additionalArgs,
			}

			err = mysql_dump.Backup(mda)
			if err != nil {
				cmd.PrintErrln(err)
				os.Exit(1)
			}
		}
	},
}

func init() {
	BackupCmd.Flags().StringVarP(&dbType, "type", "t", "", "Database type (mysql, postgres, mariadb, mongodb)")
	BackupCmd.Flags().StringVarP(&host, "host", "H", "", "Database host")
	BackupCmd.Flags().StringVarP(&port, "port", "P", "", "Database port")
	BackupCmd.Flags().StringVarP(&user, "user", "u", "", "Database user")
	BackupCmd.Flags().StringVarP(&password, "password", "p", "", "Database password")
	BackupCmd.Flags().StringVarP(&database, "database", "d", "", "Database name")

	BackupCmd.Flags().StringVarP(&output, "output", "o", "", "Output name")
	BackupCmd.Flags().BoolVarP(&compress, "compress", "c", false, "Compress the backup")
	BackupCmd.Flags().StringVar(&additionalArgs, "args", "", "Additional arguments you want to pass to the dump command")

	// postgresql flags
	BackupCmd.Flags().StringVar(&pgOutFormat, "pg-out-format", "", "PostgresSQL output format [p (plain), c (custom), d (directory), t (tar)] ")
	BackupCmd.Flags().StringVar(&pgCompressionAlgo, "pg-compression-algo", "", "PostgresSQL compression algorithm [gzip, lz4, zstd, none]")
	BackupCmd.Flags().IntVar(&pgCompressionLevel, "pg-compression-level", 0, "PostgresSQL compression level [0-9]")

	// required args
	err := BackupCmd.MarkFlagRequired("type")
	if err != nil {
		return
	}

	// add the backup command to the root command
	RootCmd.AddCommand(BackupCmd)
}
