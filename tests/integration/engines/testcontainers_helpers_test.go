//go:build integration

package engines

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresContainer encapsulates a PostgreSQL testcontainer instance with connection details.
type PostgresContainer struct {
	Container testcontainers.Container
	Host      string
	Port      string
	Username  string
	Password  string
	Database  string
}

// StartPostgres creates and starts a PostgreSQL container for integration testing.
func StartPostgres(t *testing.T, ctx context.Context) *PostgresContainer {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:17-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "testuser",
			"POSTGRES_PASSWORD": "testpass",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start postgres container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get postgres host: %v", err)
	}

	mappedPort, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("Failed to get postgres port: %v", err)
	}

	return &PostgresContainer{
		Container: container,
		Host:      host,
		Port:      mappedPort.Port(),
		Username:  "testuser",
		Password:  "testpass",
		Database:  "testdb",
	}
}

// MySQLContainer encapsulates a MySQL testcontainer instance with connection details.
type MySQLContainer struct {
	Container testcontainers.Container
	Host      string
	Port      string
	Username  string
	Password  string
	Database  string
}

// StartMySQL creates and starts a MySQL container for integration testing.
func StartMySQL(t *testing.T, ctx context.Context) *MySQLContainer {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "mysql:8.0",
		ExposedPorts: []string{"3306/tcp"},
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": "rootpass",
			"MYSQL_USER":          "testuser",
			"MYSQL_PASSWORD":      "testpass",
			"MYSQL_DATABASE":      "testdb",
		},
		WaitingFor: wait.ForLog("port: 3306  MySQL Community Server").WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start mysql container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get mysql host: %v", err)
	}

	mappedPort, err := container.MappedPort(ctx, "3306")
	if err != nil {
		t.Fatalf("Failed to get mysql port: %v", err)
	}

	return &MySQLContainer{
		Container: container,
		Host:      host,
		Port:      mappedPort.Port(),
		Username:  "testuser",
		Password:  "testpass",
		Database:  "testdb",
	}
}

// MariaDBContainer encapsulates a MariaDB testcontainer instance with connection details.
type MariaDBContainer struct {
	Container testcontainers.Container
	Host      string
	Port      string
	Username  string
	Password  string
	Database  string
}

// StartMariaDB creates and starts a MariaDB container for integration testing.
func StartMariaDB(t *testing.T, ctx context.Context) *MariaDBContainer {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "mariadb:11",
		ExposedPorts: []string{"3306/tcp"},
		Env: map[string]string{
			"MARIADB_ROOT_PASSWORD": "rootpass",
			"MARIADB_USER":          "testuser",
			"MARIADB_PASSWORD":      "testpass",
			"MARIADB_DATABASE":      "testdb",
		},
		WaitingFor: wait.ForLog("mariadbd: ready for connections").WithOccurrence(2).WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start mariadb container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get mariadb host: %v", err)
	}

	mappedPort, err := container.MappedPort(ctx, "3306")
	if err != nil {
		t.Fatalf("Failed to get mariadb port: %v", err)
	}

	return &MariaDBContainer{
		Container: container,
		Host:      host,
		Port:      mappedPort.Port(),
		Username:  "testuser",
		Password:  "testpass",
		Database:  "testdb",
	}
}

// MongoDBContainer encapsulates a MongoDB testcontainer instance with connection details.
type MongoDBContainer struct {
	Container testcontainers.Container
	Host      string
	Port      string
	Username  string
	Password  string
	Database  string
}

// StartMongoDB creates and starts a MongoDB container for integration testing.
func StartMongoDB(t *testing.T, ctx context.Context) *MongoDBContainer {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "mongo:7",
		ExposedPorts: []string{"27017/tcp"},
		Env: map[string]string{
			"MONGO_INITDB_ROOT_USERNAME": "testuser",
			"MONGO_INITDB_ROOT_PASSWORD": "testpass",
			"MONGO_INITDB_DATABASE":      "testdb",
		},
		WaitingFor: wait.ForLog("Waiting for connections").WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start mongodb container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get mongodb host: %v", err)
	}

	mappedPort, err := container.MappedPort(ctx, "27017")
	if err != nil {
		t.Fatalf("Failed to get mongodb port: %v", err)
	}

	return &MongoDBContainer{
		Container: container,
		Host:      host,
		Port:      mappedPort.Port(),
		Username:  "testuser",
		Password:  "testpass",
		Database:  "testdb",
	}
}

// ConnectionString returns the MongoDB connection URI.
func (m *MongoDBContainer) ConnectionString() string {
	return fmt.Sprintf("mongodb://%s:%s@%s:%s/%s?authSource=admin",
		m.Username, m.Password, m.Host, m.Port, m.Database)
}
