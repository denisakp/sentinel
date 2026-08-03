package db_probe

import (
	"context"
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// mongoConnect is a package-level seam so tests can inject a stub client.
// Production behavior is unchanged.
var mongoConnect = mongo.Connect

// CheckMongoConnectivity connects to the MongoDB instance at uri, pings it on
// the primary read preference, and disconnects. Failure-mode matrix is
// defined in specs/016-remove-ping-fatal/contracts/connectivity-helpers.md.
//
// CheckMongoConnectivity MUST NOT call log.Fatal*, log.Panic*, or os.Exit.
func CheckMongoConnectivity(uri string) (err error) {
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	opts := options.Client().ApplyURI(uri).SetServerAPIOptions(serverAPI)

	client, err := mongoConnect(opts)
	if err != nil {
		return fmt.Errorf("failed to connect to the MongoDB instance - %w", err)
	}
	defer func() {
		dErr := client.Disconnect(context.TODO())
		if dErr == nil {
			return
		}
		if err == nil {
			err = fmt.Errorf("disconnect after ping: %w", dErr)
			return
		}
		slog.Warn("mongo_ping_disconnect_error", "event", "mongo_ping_disconnect_error", "error", dErr.Error())
	}()

	if pingErr := client.Ping(context.TODO(), readpref.Primary()); pingErr != nil {
		return fmt.Errorf("failed to ping the MongoDB instance - %w", pingErr)
	}
	return nil
}

// ListMongoDatabases returns MongoDB database names for the given URI.
func ListMongoDatabases(uri string) ([]string, error) {
	if uri == "" {
		return nil, fmt.Errorf("mongo uri is required")
	}
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerAPIOptions(serverAPI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer func() {
		_ = client.Disconnect(context.TODO())
	}()

	names, err := client.ListDatabaseNames(context.TODO(), map[string]interface{}{})
	if err != nil {
		return nil, fmt.Errorf("failed to list MongoDB databases: %w", err)
	}
	return names, nil
}
