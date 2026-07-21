package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	var cfg config
	flag.StringVar(&cfg.suite, "suite", "", "test suite name")
	flag.StringVar(&cfg.runID, "run-id", "", "CI run identifier")
	flag.StringVar(&cfg.rawDir, "raw-dir", "", "directory containing raw diagnostics")
	flag.StringVar(&cfg.outputDir, "output-dir", "", "directory for sanitized diagnostics")
	flag.StringVar(&cfg.canaryFile, "canary-file", "", "file containing exact leak canaries")
	flag.Parse()

	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "e2e diagnostics failed")
		os.Exit(1)
	}
}
