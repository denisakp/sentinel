package db_probe

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/denisakp/sentinel/internal/ports"
)

// ProbeIncrementalCapabilities implements ports.DBProber. It queries the live
// server for the state incremental backup depends on (issue #185): these facts
// cannot be known at config-load time.
func (a *Adapter) ProbeIncrementalCapabilities(ctx context.Context, conn ports.DatabaseConfig) (*ports.IncrementalCapabilities, error) {
	switch conn.Type {
	case "mysql", "mariadb":
		return probeMySQLCapabilities(ctx, conn)
	case "postgres":
		return probePostgresCapabilities(ctx, conn)
	case "mongodb":
		return probeMongoCapabilities(ctx, conn.URI)
	default:
		return nil, ports.ErrUnsupportedEngine
	}
}

func probeMySQLCapabilities(ctx context.Context, conn ports.DatabaseConfig) (*ports.IncrementalCapabilities, error) {
	port := portStr(conn.Port)
	if port == "" {
		port = "3306"
	}
	dsn := (&mysql.Config{
		User:                 conn.Username,
		Passwd:               conn.Password,
		Net:                  "tcp",
		Addr:                 fmt.Sprintf("%s:%s", conn.Host, port),
		AllowNativePasswords: true,
	}).FormatDSN()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql connection: %w", err)
	}
	defer func() { _ = db.Close() }()

	var raw sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT @@global.log_bin").Scan(&raw); err != nil {
		return nil, fmt.Errorf("query log_bin: %w", err)
	}
	return &ports.IncrementalCapabilities{LogBinEnabled: parseBoolSetting(raw.String)}, nil
}

func probePostgresCapabilities(ctx context.Context, conn ports.DatabaseConfig) (*ports.IncrementalCapabilities, error) {
	dbname := conn.Database
	if dbname == "" || dbname == "*" {
		dbname = "postgres"
	}
	db, err := sql.Open("postgres", buildPostgresDSN(conn.Host, conn.Port, conn.Username, dbname, conn.Password))
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}
	defer func() { _ = db.Close() }()

	var versionNum string
	if err := db.QueryRowContext(ctx, "SHOW server_version_num").Scan(&versionNum); err != nil {
		return nil, fmt.Errorf("query server_version_num: %w", err)
	}
	major, err := parsePostgresMajor(versionNum)
	if err != nil {
		return nil, err
	}
	caps := &ports.IncrementalCapabilities{VersionMajor: major}
	if major >= 17 {
		var summarize string
		if err := db.QueryRowContext(ctx, "SHOW summarize_wal").Scan(&summarize); err != nil {
			return nil, fmt.Errorf("query summarize_wal: %w", err)
		}
		caps.WALSummaryEnabled = parseBoolSetting(summarize)
	}
	return caps, nil
}

func probeMongoCapabilities(ctx context.Context, uri string) (*ports.IncrementalCapabilities, error) {
	if strings.TrimSpace(uri) == "" {
		return nil, fmt.Errorf("mongo uri is required")
	}
	client, err := mongoConnect(options.Client().ApplyURI(uri).SetServerAPIOptions(options.ServerAPI(options.ServerAPIVersion1)))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()

	var hello bson.M
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello); err != nil {
		return nil, fmt.Errorf("mongo hello: %w", err)
	}
	_, isReplica := hello["setName"]
	caps := &ports.IncrementalCapabilities{ReplicaSet: isReplica}
	if !isReplica {
		return caps, nil
	}

	oplog := client.Database("local").Collection("oplog.rs")
	first, err := oplogEdgeTS(ctx, oplog, 1)
	if err != nil {
		return nil, fmt.Errorf("read first oplog entry: %w", err)
	}
	last, err := oplogEdgeTS(ctx, oplog, -1)
	if err != nil {
		return nil, fmt.Errorf("read last oplog entry: %w", err)
	}
	caps.OplogWindowHours = oplogWindowHours(first, last)
	return caps, nil
}

func oplogEdgeTS(ctx context.Context, c *mongo.Collection, order int) (uint32, error) {
	var doc struct {
		TS bson.Timestamp `bson:"ts"`
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "$natural", Value: order}})
	if err := c.FindOne(ctx, bson.D{}, opts).Decode(&doc); err != nil {
		return 0, err
	}
	return doc.TS.T, nil
}

// oplogWindowHours converts first/last oplog timestamps (unix seconds) into
// whole hours; a reversed or equal pair yields 0.
func oplogWindowHours(firstSec, lastSec uint32) int {
	if lastSec <= firstSec {
		return 0
	}
	return int((time.Duration(lastSec-firstSec) * time.Second).Hours())
}

// parseBoolSetting interprets a server variable value (1/0, ON/OFF, true/false).
func parseBoolSetting(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "on", "true", "yes":
		return true
	default:
		return false
	}
}

// parsePostgresMajor derives the major version from server_version_num
// (e.g. "170002" -> 17, "90624" -> 9).
func parsePostgresMajor(versionNum string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(versionNum))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("unparseable server_version_num %q", versionNum)
	}
	return n / 10000, nil
}
