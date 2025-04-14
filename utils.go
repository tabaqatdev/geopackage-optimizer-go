package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

func init() {
	// Add executable directory to PATH for Windows to help find DLLs
	if runtime.GOOS == "windows" {
		execPath, err := os.Executable()
		if err == nil {
			execDir := filepath.Dir(execPath)
			log.Printf("Adding executable directory to PATH: %s", execDir)
			// Add it as the first entry to ensure our DLLs are found first
			os.Setenv("PATH", execDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			
			// On Windows, we may need to preload dependencies in the correct order
			// This is a workaround for some environments where dynamic linking fails
			preloadDependencies(execDir)
		} else {
			log.Printf("Warning: Could not determine executable path: %s", err)
		}
	}
}

// preloadDependencies attempts to preload critical DLLs in the correct order
// This is Windows-specific and helps with SpatiaLite loading issues
func preloadDependencies(execDir string) {
	if runtime.GOOS != "windows" {
		return
	}
	
	// List of DLLs to preload in order (from base dependencies to higher-level ones)
	dlls := []string{
		"libwinpthread-1.dll",
		"libgcc_s_seh-1.dll",
		"libstdc++-6.dll",
		"libsqlite3-0.dll",
		"libgeos.dll",
		"libgeos_c.dll",
		"libspatialite-5.dll",
		"mod_spatialite.dll",
	}
	
	for _, dll := range dlls {
		dllPath := filepath.Join(execDir, dll)
		_, err := os.Stat(dllPath)
		if err == nil {
			log.Printf("Found dependency: %s", dll)
		} else {
			log.Printf("Missing expected dependency: %s", dll)
		}
	}
}

func registerDriver(driverName string, extensions []string) {
	for _, driver := range sql.Drivers() {
		if driver == driverName {
			return
		}
	}
	sql.Register(driverName, &sqlite3.SQLiteDriver{
		Extensions: extensions,
	})
}

func openDb(sourceGeopackage string) *sql.DB {
	driverName := "sqlite3_with_extensions"
	
	// Register driver with SpatiaLite extension
	registerDriver(
		driverName,
		[]string{
			"mod_spatialite",
		},
	)

	// Use URI connection string with flags to help locate extensions
	connString := fmt.Sprintf("%s?_load_extension=1", sourceGeopackage)
	db, err := sql.Open(driverName, connString)
	if err != nil {
		log.Fatalf("error opening source GeoPackage: %s", err)
	}

	// Initialize the SpatiaLite extension with different strategies
	spatialiteLoaded := false
	
	// Try options in order of most likely to succeed
	loadOptions := []struct {
		name string
		path string
	}{
		{"Default", "mod_spatialite"},
		{"Relative path", "./mod_spatialite"},
		{"Full path Windows", filepath.Join(filepath.Dir(sourceGeopackage), "mod_spatialite")},
	}
	
	if runtime.GOOS == "windows" {
		// Add Windows-specific options
		exePath, _ := os.Executable()
		exeDir := filepath.Dir(exePath)
		
		// Add additional Windows-specific options
		loadOptions = append(loadOptions, 
			struct{name string; path string}{"Explicit extension", "mod_spatialite.dll"},
			struct{name string; path string}{"Exe directory", filepath.Join(exeDir, "mod_spatialite")},
			struct{name string; path string}{"Exe directory with ext", filepath.Join(exeDir, "mod_spatialite.dll")},
			struct{name string; path string}{"Direct DLL reference", exeDir + string(os.PathListSeparator) + "mod_spatialite.dll"},
		)
	}
	
	var lastErr error
	// We'll retry the loading a few times with a short delay
	// This can help in some Windows environments where DLL loading has timing issues
	maxRetries := 3
	
	for attempt := 0; attempt < maxRetries && !spatialiteLoaded; attempt++ {
		if attempt > 0 {
			log.Printf("Retrying SpatiaLite loading, attempt %d of %d", attempt+1, maxRetries)
			time.Sleep(500 * time.Millisecond)
		}
		
		for _, option := range loadOptions {
			_, err = db.Exec(fmt.Sprintf("SELECT load_extension('%s')", option.path))
			if err == nil {
				log.Printf("Successfully loaded SpatiaLite extension with option: %s", option.name)
				spatialiteLoaded = true
				break
			}
			lastErr = err
			log.Printf("Failed to load SpatiaLite with option '%s': %s", option.name, err)
		}
	}
	
	if !spatialiteLoaded {
		log.Printf("Warning: Could not load SpatiaLite extension: %s", lastErr)
		log.Printf("Will attempt to continue without SpatiaLite functionality")
		
		if runtime.GOOS == "windows" {
			log.Printf("Windows troubleshooting tips:")
			log.Printf("1. Ensure all DLLs are in the same directory as the executable")
			log.Printf("2. Verify required Visual C++ Redistributable is installed")
			log.Printf("3. Try renaming 'libgeos.dll' to 'geos.dll' and 'libgeos_c.dll' to 'geos_c.dll'")
			
			// Perform diagnostics to help troubleshoot
			exePath, _ := os.Executable()
			exeDir := filepath.Dir(exePath)
			log.Printf("Executable directory: %s", exeDir)
			log.Printf("Current PATH: %s", os.Getenv("PATH"))
			
			// Check for existence of key DLLs
			dlls := []string{"mod_spatialite.dll", "libspatialite-5.dll", "libgeos.dll", "libgeos_c.dll", "libsqlite3-0.dll"}
			for _, dll := range dlls {
				dllPath := filepath.Join(exeDir, dll)
				if _, err := os.Stat(dllPath); err == nil {
					log.Printf("Found DLL: %s", dllPath)
				} else {
					log.Printf("Missing DLL: %s", dllPath)
				}
			}
		}
	} else {
		// Verify SpatiaLite loaded correctly by checking its version
		var version string
		err = db.QueryRow("SELECT spatialite_version()").Scan(&version)
		if err == nil {
			log.Printf("SpatiaLite version: %s", version)
		}
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
