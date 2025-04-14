package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/mattn/go-sqlite3"
)

func init() {
	// Basic platform-agnostic initialization
	// The platform-specific initialization is in platform_*.go files
}

func registerDriver(driverName string, extensions []string) {
	for _, driver := range sql.Drivers() {
		if driver == driverName {
			return
		}
	}
	sql.Register(driverName, &sqlite3.SQLiteDriver{
		Extensions: extensions,
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			// This is a hook that runs when a new connection is established
			// We can use it to initialize extensions directly
			return nil
		},
	})
}

func openDb(sourceGeopackage string) *sql.DB {
	// Preload dependencies before connecting
	// The platform-specific preloadDependencies implementation is in platform_*.go files

	driverName := "sqlite3_with_extensions"
	
	// Register driver with SpatiaLite extension and connect hook
	registerDriver(
		driverName,
		[]string{
			"mod_spatialite",
		},
	)

	// Try different connection string formats
	connStrings := []string{
		fmt.Sprintf("file:%s?_load_extension=1&_sqlite_extensions=1", sourceGeopackage),
		fmt.Sprintf("%s?_load_extension=1", sourceGeopackage),
		fmt.Sprintf("file:%s?mode=rw&_fk=1", sourceGeopackage),
	}
	
	var db *sql.DB
	var err error
	var openErr error
	
	// Try each connection string until one works
	for _, connString := range connStrings {
		db, openErr = sql.Open(driverName, connString)
		if openErr == nil {
			log.Printf("Successfully opened database with connection string: %s", connString)
			break
		}
		log.Printf("Failed to open database with connection string: %s - %s", connString, openErr)
	}
	
	if openErr != nil {
		log.Fatalf("error opening source GeoPackage: %s", openErr)
	}

	// Enable extension loading first - this is critical
	db.Exec("PRAGMA foreign_keys = ON;")
	db.Exec("PRAGMA trusted_schema = 1;")
	
	// Directly try to enable extension loading
	// The platform-specific extension loading implementation is in platform_*.go files

	// Initialize the SpatiaLite extension with different strategies
	spatialiteLoaded := false
	
	// Verify if SpatiaLite is loaded
	var spatialiteVersion string
	err = db.QueryRow("SELECT sqlite_version()").Scan(&spatialiteVersion)
	if err == nil {
		log.Printf("SQLite version: %s", spatialiteVersion)
	} else {
		log.Printf("Error getting SQLite version: %s", err)
	}
	
	// Try to check SpatiaLite version
	err = db.QueryRow("SELECT spatialite_version()").Scan(&spatialiteVersion)
	if err == nil {
		log.Printf("SpatiaLite successfully loaded, version: %s", spatialiteVersion)
		spatialiteLoaded = true
	} else {
		log.Printf("SpatiaLite not yet loaded: %s", err)
	}
	
	// If not loaded yet, try another approach
	// The platform-specific SpatiaLite loading implementation is in platform_*.go files

	if !spatialiteLoaded {
		log.Printf("Warning: Could not load SpatiaLite extension")
		log.Printf("Will attempt to continue without SpatiaLite functionality")
	}

	return db
}

func getTableNames(db *sql.DB) []string {
	rows, err := db.Query("select table_name from gpkg_contents")
	if err != nil {
		log.Fatalf("error selecting gpkg_contents: %s", err)
	}

	var tableNames []string

	for rows.Next() {
		var table_name string
		err = rows.Scan(&table_name)
		if err != nil {
			log.Fatal(err)
		}
		tableNames = append(tableNames, table_name)
	}

	return tableNames
}

func createIndex(tableName string, columnNames []string, indexName string, unique bool, db *sql.DB) {
	if indexName == "" {
		indexName = fmt.Sprintf("%s_%s_index", tableName, strings.Join(columnNames, "_"))
	}

	var queryStr string
	if unique {
		queryStr = "CREATE UNIQUE INDEX %s ON %s(%s);"
	} else {
		queryStr = "CREATE INDEX %s ON %s(%s);"
	}

	query := fmt.Sprintf(queryStr, indexName, tableName, strings.Join(columnNames, ","))
	log.Printf("executing query: %s\n", query)

	_, err := db.Exec(query)
	if err != nil {
		log.Fatalf("error creating index: %s", err)
	}
}

func setColumnValue(tableName string, columnName string, value string, db *sql.DB) {
	query := fmt.Sprintf("UPDATE '%s' SET '%s' = %s;", tableName, columnName, value)
	log.Printf("executing query: %s\n", query)

	_, err := db.Exec(query)
	if err != nil {
		log.Fatalf("error setting value '%s' to column '%s': '%s'", value, columnName, err)
	}
}

func addColumn(tableName string, columnName string, columnType string, db *sql.DB) {
	query := fmt.Sprintf("ALTER TABLE '%s' ADD '%s' %s;", tableName, columnName, columnType)
	log.Printf("executing query: %s\n", query)

	_, err := db.Exec(query)
	if err != nil {
		log.Fatalf("error adding column '%s': '%s'", columnName, err)
	}
}

func executeQuery(query string, db *sql.DB) {
	query = fmt.Sprintf("%s;", query)
	log.Printf("executing query: %s\n", query)

	_, err := db.Exec(query)
	if err != nil {
		log.Fatalf("error executing query: '%s'", err)
	}
}

func analyze(db *sql.DB) {
	_, err := db.Exec("ANALYZE")
	if err != nil {
		log.Fatalf("error running analyze: %s", err)
	}
}
