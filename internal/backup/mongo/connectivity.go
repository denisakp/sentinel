package mongo

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"log"
)

// CheckConnectivity checks the connectivity to the MongoDB instance
func CheckConnectivity(uri string) error {
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)             // enable the server API version 1
	opts := options.Client().ApplyURI(uri).SetServerAPIOptions(serverAPI) // set the server API options

	// create a new client and connect to the MongoDB instance
	client, err := mongo.Connect(opts)
	if err != nil {
		return fmt.Errorf("failed to connect to the MongoDB instance - %w", err)
	}
	defer func() {
		if err = client.Disconnect(context.TODO()); err != nil {
			log.Panic(err)
		}
	}()

	// ping the MongoDB instance
	if err = client.Ping(context.TODO(), readpref.Primary()); err != nil {
		return fmt.Errorf("failed to ping the MongoDB instance - %w", err)
	}

	return nil
}
