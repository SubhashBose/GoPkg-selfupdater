# Go Binary self-updating module.

This Golang pakage automates the checking of the latest version from the GitHub release page. If a newer version is found, it will download the appropriate binary for the OS and architecture, and will self-update the executing binary.

To configure the required parameters
```go
cfg := selfupdate.Config{
		RepoURL:        "https://github.com/SubhashBose/RouteMUX",   // Github repo containig the binary releases
		BinaryPrefix:   "routemux-",   // Prefix in the release binary file names to match with 
		OSSep:          "-",           // Separator between OS and Arch in release binary file name
		CurrentVersion: version,       // version variable contains the current version of the binary
		// HTTPClient: myCustomClient, // optional
	}

res, err := selfupdate.Update(cfg)  // execute the update process
```

## Example to implement the module in Go programs

``` go
// example/main.go — shows how to use the selfupdate package in RouteMUX
// (or any other program).  Drop selfupdate/ next to your module and import it.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/SubhashBose/GoModule-selfupdater"
)

// Set at build time:
//
//	go build -ldflags "-X main.version=1.0.0" .
var version = "1.0.0"

func main() {
	checkUpdate := flag.Bool("update", false, "check for and apply updates, then exit")
	flag.Parse()

	if *checkUpdate {
		runUpdate()
		return
	}

	// … normal program logic …
	fmt.Println("RouteMUX", version, "running")
}

func runUpdate() {
	cfg := selfupdate.Config{
		RepoURL:        "https://github.com/SubhashBose/RouteMUX",
		BinaryPrefix:   "routemux-",
		OSSep:          "-",
		CurrentVersion: version,
		// HTTPClient: myCustomClient, // optional
	}

	fmt.Printf("Current version: %s\nChecking for updates…", version)
 
	res, err := selfupdate.Update(cfg)

	if res.LatestVersion != "" {
		fmt.Printf(" Latest version: %s\n", res.LatestVersion)
	} else {
		fmt.Printf("\n")
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "Update failed:", err)
		os.Exit(1)
	}

	if !res.Updated {
		fmt.Printf("Already up to date (latest: %s)\n", res.LatestVersion)
		return
	}

	fmt.Printf("Successfully updated to v%s (asset: %s)\nPlease restart the program.\n",
		res.LatestVersion, res.AssetName)
}

```
