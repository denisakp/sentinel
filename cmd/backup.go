package cmd

import (
	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/pkg/backup/mariadb_dump"
	"github.com/denisakp/sentinel/pkg/backup/mongo_dump"
	"github.com/denisakp/sentinel/pkg/backup/mysql_dump"
	"github.com/denisakp/sentinel/pkg/backup/pg_dump"
	"github.com/spf13/cobra"
	"os"
)

var dbType, host, port, user, password, database, output, additionalArgs, pgOutFormat, pgCompressionAlgo, uri string
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

		host, _ = cmd.Flags().GetString("host")           // get the host flag value
		port, _ = cmd.Flags().GetString("port")           // get the port flag value
		user, _ = cmd.Flags().GetString("user")           // get the user flag value
		password, _ = cmd.Flags().GetString("password")   // get the password flag value
		database, _ = cmd.Flags().GetString("database")   // get the database flag value
		output, _ = cmd.Flags().GetString("output")       // get the output flag value
		additionalArgs, _ = cmd.Flags().GetString("args") // get the args flag value

		if dbType == "postgres" {
			compress, _ = cmd.Flags().GetBool("compress")                       // get the compress flag value
			pgOutFormat, _ = cmd.Flags().GetString("pg-out-format")             // get the pg-out-format flag value
			pgCompressionAlgo, _ = cmd.Flags().GetString("pg-compression-algo") // get the pg-compression-algo flag value
			pgCompressionLevel, _ = cmd.Flags().GetInt("pg-compression-level")  // get the pg-compression-level flag value

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

		if dbType == "mariadb" {
			mda := &mariadb_dump.MariaDBDumpArgs{
				Host:           host,
				Port:           port,
				Username:       user,
				Password:       password,
				Database:       database,
				OutName:        output,
				AdditionalArgs: additionalArgs,
			}

			err = mariadb_dump.Backup(mda)
			if err != nil {
				cmd.PrintErrln(err)
				os.Exit(1)
			}
		}

		if dbType == "mongodb" {
			compress, _ = cmd.Flags().GetBool("compress") // get the compress flag value
			uri, _ := cmd.Flags().GetString("uri")        // get the uri flag value

			da := &mongo_dump.DumpMongoArgs{
				Compress:       compress,
				AdditionalArgs: additionalArgs,
				OutName:        output,
				Uri:            uri,
			}

			err = mongo_dump.Backup(da)
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

	// mongodb flags
	BackupCmd.Flags().StringVarP(&uri, "uri", "", "", "MongoDB URI")

	// required args
	err := BackupCmd.MarkFlagRequired("type")
	if err != nil {
		return
	}

	// add the backup command to the root command
	RootCmd.AddCommand(BackupCmd)
}
