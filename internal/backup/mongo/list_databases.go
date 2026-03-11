package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ListDatabases returns MongoDB database names for the given URI.
func ListDatabases(uri string) ([]string, error) {
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
