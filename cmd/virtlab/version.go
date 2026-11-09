package main

import (
	"flag"
	"fmt"

	"github.com/y0n1/virtlab/internal/version"
)

func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output version information in JSON format")
	_ = fs.Parse(args)

	info := version.Get()

	if *jsonOutput {
		jsonStr, err := info.JSON()
		if err != nil {
			return fmt.Errorf("format version json: %w", err)
		}
		fmt.Println(jsonStr)
		return nil
	}

	fmt.Println(info.String())
	return nil
}
