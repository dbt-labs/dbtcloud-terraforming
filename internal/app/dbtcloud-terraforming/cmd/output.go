package cmd

import (
	"io"
	"os"

	"github.com/hashicorp/hcl/v2/hclwrite"
)

var outputFile string

// getOutputWriter returns an io.Writer based on the --output flag
// If no output file is specified, returns os.Stdout
// If an output file is specified, opens the file and returns it
// The caller is responsible for closing the file if one was opened
func getOutputWriter() (io.Writer, func() error, error) {
	if outputFile == "" {
		return os.Stdout, func() error { return nil }, nil
	}

	f, err := os.Create(outputFile)
	if err != nil {
		return nil, nil, err
	}

	return f, f.Close, nil
}

// writeOutput writes the given HCL file to the configured output destination
func writeOutput(f *hclwrite.File) error {
	writer, closer, err := getOutputWriter()
	if err != nil {
		return err
	}
	defer closer()

	_, err = writer.Write(f.Bytes())
	return err
}

// writeString writes the given string to the configured output destination
func writeString(s string) error {
	writer, closer, err := getOutputWriter()
	if err != nil {
		return err
	}
	defer closer()

	_, err = writer.Write([]byte(s))
	return err
}
