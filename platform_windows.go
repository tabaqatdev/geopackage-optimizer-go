//go:build windows
// +build windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// Windows-specific DLL loading for SpatiaLite
var (
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	loadLibraryEx              = kernel32.NewProc("LoadLibraryExW")
	getProcAddress             = kernel32.NewProc("GetProcAddress")
	LOAD_WITH_ALTERED_SEARCH_PATH = 0x00000008
)

// Important functions we might need to call directly
var (
	// Will be initialized during preloadDependencies
	spatialiteDLL  *syscall.LazyDLL
	spatialiteInit *syscall.LazyProc
)

// preloadDependencies tries to preload required DLLs on Windows
// to ensure they're available before SQLite tries to load them
func preloadDependencies() {
	log.Println("Preloading Windows dependencies...")
	
	// Add executable directory to PATH
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Warning: Could not get executable path: %s", err)
		return
	}
	
	execDir := filepath.Dir(execPath)
	log.Printf("Adding executable directory to PATH: %s", execDir)
	
	// Update PATH to include the executable directory
	currentPath := os.Getenv("PATH")
	newPath := execDir
	if currentPath != "" {
		newPath = newPath + string(os.PathListSeparator) + currentPath
	}
	os.Setenv("PATH", newPath)
	
	// Enable loading extensions by setting SQLite environment variable
	os.Setenv("SQLITE_ENABLE_LOAD_EXTENSION", "1")
	
	// List of DLLs to preload in correct dependency order
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
	
	// Try to load each DLL directly using the new LazyDLL approach
	for _, dll := range dlls {
		dllPath := filepath.Join(execDir, dll)
		
		// Skip if file doesn't exist
		if _, err := os.Stat(dllPath); os.IsNotExist(err) {
			log.Printf("DLL not found: %s", dllPath)
			continue
		}
		
		// First try loading directly by filename (might pick up from PATH)
		lazyDLL := syscall.NewLazyDLL(dll)
		err := lazyDLL.Load()
		if err == nil {
			log.Printf("Found dependency via LazyDLL: %s", dll)
			
			// Store the spatialite module for direct function calls if needed
			if dll == "mod_spatialite.dll" {
				spatialiteDLL = lazyDLL
				spatialiteInit = spatialiteDLL.NewProc("sqlite3_spatialite_init")
				log.Printf("Successfully loaded spatialite_init function")
			}
			continue
		}
		
		// If that fails, try with full path 
		lazyDLL = syscall.NewLazyDLL(dllPath)
		err = lazyDLL.Load()
		if err == nil {
			log.Printf("Found dependency via LazyDLL with path: %s", dllPath)
			
			// Store the spatialite module for direct function calls if needed
			if dll == "mod_spatialite.dll" {
				spatialiteDLL = lazyDLL
				spatialiteInit = spatialiteDLL.NewProc("sqlite3_spatialite_init")
				log.Printf("Successfully loaded spatialite_init function via path")
			}
			continue
		}
		
		// If everything else fails, try with LoadLibraryEx
		dllPathW, _ := syscall.UTF16PtrFromString(dllPath)
		handle, _, err := loadLibraryEx.Call(
			uintptr(unsafe.Pointer(dllPathW)),
			0,
			uintptr(LOAD_WITH_ALTERED_SEARCH_PATH),
		)
		
		if handle != 0 {
			log.Printf("Found dependency via LoadLibraryEx: %s", dll)
		} else {
			log.Printf("Failed to preload: %s - %s", dll, err)
		}
	}
	
	// Try to directly initialize SpatiaLite if we loaded the DLL successfully
	if spatialiteInit != nil {
		log.Printf("Attempting direct SpatiaLite initialization...")
		ret, _, err := spatialiteInit.Call(0) // Pass 0 as the db handle to just test loading
		if ret != 0 || err != syscall.Errno(0) {
			log.Printf("Direct SpatiaLite initialization returned: %d, err: %v", ret, err)
		} else {
			log.Printf("Direct SpatiaLite initialization succeeded!")
		}
	}
	
	// Create alternative DLL names for libraries that might be hardcoded differently
	// This handles cases where the library looks for "geos.dll" instead of "libgeos.dll"
	alternativeDLLs := map[string]string{
		"libgeos.dll":   "geos.dll",
		"libgeos_c.dll": "geos_c.dll",
	}
	
	for original, alternative := range alternativeDLLs {
		originalPath := filepath.Join(execDir, original)
		alternativePath := filepath.Join(execDir, alternative)
		
		// Skip if original doesn't exist or alternative already exists
		if _, err := os.Stat(originalPath); os.IsNotExist(err) {
			continue
		}
		if _, err := os.Stat(alternativePath); err == nil {
			continue // Alternative already exists
		}
		
		// Create a copy with the alternative name
		originalBytes, err := os.ReadFile(originalPath)
		if err != nil {
			log.Printf("Failed to read %s: %v", originalPath, err)
			continue
		}
		
		err = os.WriteFile(alternativePath, originalBytes, 0755)
		if err != nil {
			log.Printf("Failed to create %s: %v", alternativePath, err)
			continue
		}
		
		log.Printf("Created alternative DLL name: %s -> %s", original, alternative)
	}
	
	// Extra diagnostics
	log.Printf("Go version: %s", runtime.Version())
	log.Printf("Windows architecture: %s", runtime.GOARCH)
}

// loadSpatialite attempts to directly load the SpatiaLite extension
// using various techniques specific to Windows
func loadSpatialite(dbPath string) {
	if spatialiteDLL == nil || spatialiteInit == nil {
		log.Printf("Cannot load SpatiaLite directly: DLL not properly loaded")
		return
	}
	
	log.Printf("Attempting direct Windows-specific SpatiaLite loading...")
	
	// Explicit fallback technique for extreme cases
	dllDir, _ := os.Executable()
	dllDir = filepath.Dir(dllDir)
	
	// Database path info for diagnostics
	dbName := filepath.Base(dbPath)
	log.Printf("Database info: name=%s, path=%s", dbName, dbPath)
	
	log.Printf("Will try both direct SQLite loading and Windows-specific loading")
	
	// The other loading approaches are handled in utils.go
}
