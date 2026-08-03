package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/migration"
)

func main() {
	reportPath := flag.String("report", "", "compat-report JSON used to build a draft manifest")
	inputPath := flag.String("input", "", "draft manifest JSON to validate or finalize")
	baselinePath := flag.String("baseline", "", "unaltered hashed draft required when finalizing edited decisions")
	outputPath := flag.String("output", "", "output JSON path (required for draft/finalize)")
	finalize := flag.Bool("finalize", false, "require all manual decisions and freeze the manifest hash")
	flag.Parse()
	if err := run(*reportPath, *inputPath, *baselinePath, *outputPath, *finalize); err != nil {
		fmt.Fprintln(os.Stderr, "migration-manifest:", err)
		os.Exit(1)
	}
}

func run(reportPath, inputPath, baselinePath, outputPath string, finalize bool) error {
	if (reportPath == "") == (inputPath == "") {
		return errors.New("provide exactly one of -report or -input")
	}
	if outputPath == "" {
		return errors.New("-output is required")
	}
	var manifest migration.Manifest
	if reportPath != "" {
		if finalize {
			return errors.New("-finalize requires -input")
		}
		var report migration.CompatReport
		if err := readJSON(reportPath, &report, false); err != nil {
			return err
		}
		var err error
		manifest, err = migration.BuildDraft(report)
		if err != nil {
			return err
		}
	} else {
		if err := readJSON(inputPath, &manifest, true); err != nil {
			return err
		}
		var err error
		if finalize {
			if baselinePath == "" {
				return errors.New("-baseline is required with -finalize")
			}
			var baseline migration.Manifest
			if err := readJSON(baselinePath, &baseline, true); err != nil {
				return err
			}
			manifest, err = migration.FinalizeAgainstBaseline(manifest, baseline)
		} else {
			err = migration.Validate(manifest, false)
		}
		if err != nil {
			return err
		}
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	for _, source := range []string{reportPath, inputPath, baselinePath} {
		if source == "" {
			continue
		}
		absSource, err := filepath.Abs(source)
		if err != nil {
			return err
		}
		if absSource == absOutput {
			return errors.New("output must not overwrite its input")
		}
	}
	return os.WriteFile(absOutput, content, 0o600)
}

func readJSON(path string, target any, strict bool) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode %s: multiple JSON values are not allowed", path)
		}
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
