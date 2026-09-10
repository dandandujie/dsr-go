package main

import "embed"

// demoAssets holds the demo page's static files.
//
//go:embed static
var demoAssets embed.FS
