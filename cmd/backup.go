package cmd

import (
	"github.com/denisakp/sentinel/internal/backup"
	"github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/pkg/backup/mariadb_dump"
	"github.com/denisakp/sentinel/pkg/backup/mongo_dump"
	"github.com/denisakp/sentinel/pkg/backup/mysql_dump"
	"github.com/denisakp/sentinel/pkg/backup/pg_dump"
	"github.com/spf13/cobra"
	"os"
)

var dbType, host, port, user, password, database,
	pgOutFormat, pgCompressionAlgo, uri,
	output, storageType, localPath, gDriveSaFile, gDriveFolderId,
	additionalArgs string
var compress bool
var pgCompressionLevel int
var err error

var BackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup your database",
	Long:  "Backup your database with the required options depending on the database type",
	Run: func(cmd *cobra.Command, args []string) {
		dbType, _ = cmd.Flags().GetString("type")

		// validate the database type
		if err = backup.ValidateDbType(dbType); err != nil {
			cmd.PrintErrln(err)
			return
		}

		host, _ = cmd.Flags().GetString("host")           // get the host flag value
		port, _ = cmd.Flags().GetString("port")           // get the port flag value
		user, _ = cmd.Flags().GetString("user")           // get the user flag value
		password, _ = cmd.Flags().GetString("password")   // get the password flag value
		database, _ = cmd.Flags().GetString("database")   // get the database flag value
		additionalArgs, _ = cmd.Flags().GetString("args") // get the args flag value

		// storage
		storageType, _ = cmd.Flags().GetString("storage")  // get the storage flag value
		localPath, _ = cmd.Flags().GetString("local-path") // get the local-path flag value
		output, _ = cmd.Flags().GetString("output")        // get the output flag value
		// google drive
		gDriveFolderId, _ = cmd.Flags().GetString("gdrive-folder-id")
		gDriveSaFile, _ = cmd.Flags().GetString("gdrive-sa-file")

		params := &storage.Params{
			StorageType:          storageType,
			LocalPath:            localPath,
			OutName:              output,
			GoogleServiceAccount: gDriveSaFile,
			GoogleDriveFolderId:  gDriveFolderId,
		}

		if err = storage.ValidateStorage(params); err != nil {
			cmd.PrintErrln(err)
			return
		}

		// validate the storage parameters

		if dbType == "postgres" {
			compress, _ = cmd.Flags().GetBool("compress")                       // get the compress flag value
			pgOutFormat, _ = cmd.Flags().GetString("pg-out-format")             // get the pg-out-format flag value
			pgCompressionAlgo, _ = cmd.Flags().GetString("pg-compression-algo") // get the pg-compression-algo flag value
			pgCompressionLevel, _ = cmd.Flags().GetInt("pg-compression-level")  // get the pg-compression-level flag value

			pda := &pg_dump.PgDumpArgs{
				Host:                 host,
				Port:                 port,
				Username:             user,
				Password:             password,
				Database:             database,
				PgOutFormat:          pgOutFormat,
				Compress:             compress,
				CompressionAlgorithm: pgCompressionAlgo,
				CompressionLevel:     pgCompressionLevel,
				AdditionalArgs:       additionalArgs,
				Storage:              params,
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
				AdditionalArgs: additionalArgs,
				Storage:        params,
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
				AdditionalArgs: additionalArgs,
				Storage:        params,
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
				Uri:            uri,
				Storage:        params,
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

	BackupCmd.Flags().StringVarP(&host, "host", "H", "127.0.0.1", "Database host")
	BackupCmd.Flags().StringVarP(&port, "port", "P", "", "Database port")
	BackupCmd.Flags().StringVarP(&user, "user", "u", "root", "Database user")
	BackupCmd.Flags().StringVarP(&password, "password", "p", "", "Database password")
	BackupCmd.Flags().StringVarP(&database, "database", "d", "", "Database name")

	BackupCmd.Flags().BoolVarP(&compress, "compress", "c", false, "Compress the backup")
	BackupCmd.Flags().StringVar(&additionalArgs, "args", "", "Additional arguments you want to pass to the dump command")

	// postgresql flags
	BackupCmd.Flags().StringVar(&pgOutFormat, "pg-out-format", "", "PostgresSQL output format [p (plain), c (custom), d (directory), t (tar)] ")
	BackupCmd.Flags().StringVar(&pgCompressionAlgo, "pg-compression-algo", "", "PostgresSQL compression algorithm [gzip, lz4, zstd, none]")
	BackupCmd.Flags().IntVar(&pgCompressionLevel, "pg-compression-level", 1, "PostgresSQL compression level [1-9]")

	// mongodb flags
	BackupCmd.Flags().StringVarP(&uri, "uri", "", "mongodb://localhost:27017", "MongoDB URI")

	// storage flags
	BackupCmd.Flags().StringVarP(&storageType, "storage", "s", "local", "storage type (local, s3, gcs, google-drive)")
	BackupCmd.Flags().StringVarP(&localPath, "local-path", "", "", "Local path to store the backup")
	BackupCmd.Flags().StringVarP(&output, "output", "o", "", "Output name")
	//google drive
	BackupCmd.Flags().StringVarP(&gDriveFolderId, "gdrive-folder-id", "", "", "Google Drive folder ID")
	BackupCmd.Flags().StringVarP(&gDriveSaFile, "gdrive-sa-file", "", "", "Google Drive service account file")

	// required args
	err := BackupCmd.MarkFlagRequired("type")
	if err != nil {
		return
	}

	// add the backup command to the root command
	RootCmd.AddCommand(BackupCmd)
}
