// Command server is the Imagen backend: it serves the static frontend, exposes
// the JSON API, proxies image generation to OpenRouter, runs the Telegram bot
// front-end, and stores generated images per authenticated user.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply database migrations and exit without serving")
	flag.Parse()

	var err error
	if *migrateOnly {
		err = runMigrateOnly()
	} else {
		err = run()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
