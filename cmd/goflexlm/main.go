// Command goflexlm converts FlexNet debug logs and license files to JSON.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/danofsteel32/goflexlm"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := func() {
		fmt.Fprintln(stderr, "usage: goflexlm <command> [options] [FILE|-]")
		fmt.Fprintln(stderr, "\nCommands:")
		fmt.Fprintln(stderr, "  lmgrd    Convert a debug log to JSON Lines")
		fmt.Fprintln(stderr, "  license  Convert a license file to JSON")
		fmt.Fprintln(stderr, "\nUse goflexlm <command> --help for command help.")
	}
	if len(args) == 0 {
		usage()
		return 2
	}
	command := args[0]
	var parse func(io.Reader, io.Writer, io.Writer) int
	switch command {
	case "lmgrd":
		parse = runLog
	case "license":
		parse = runLicense
	case "-h", "--help":
		usage()
		return 0
	default:
		fmt.Fprintf(stderr, "goflexlm: unknown command %q\n", command)
		usage()
		return 2
	}

	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(stderr, "usage: goflexlm %s [options] [FILE|-]\n", command)
		fmt.Fprintln(stderr, "Read FILE, or standard input when FILE is omitted or is -.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	args = flags.Args()
	if len(args) > 1 {
		flags.Usage()
		return 2
	}

	input := stdin
	if len(args) == 1 && args[0] != "-" {
		file, err := os.Open(args[0])
		if err != nil {
			fmt.Fprintf(stderr, "goflexlm: %v\n", err)
			return 1
		}
		defer file.Close()
		input = file
	}

	return parse(input, stdout, stderr)
}

func runLicense(input io.Reader, stdout, stderr io.Writer) int {
	document, err := goflexlm.ParseLicenseFile(input)
	if err != nil {
		fmt.Fprintf(stderr, "goflexlm: %v\n", err)
		return 1
	}
	if len(document.Servers)+len(document.Vendors)+len(document.Features) == 0 && !document.UseServer {
		fmt.Fprintln(stderr, "goflexlm: no parseable license records")
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(document); err != nil {
		fmt.Fprintf(stderr, "goflexlm: write output: %v\n", err)
		return 1
	}
	return 0
}

func runLog(input io.Reader, stdout, stderr io.Writer) int {
	decoder := goflexlm.NewDecoder(input)
	encoder := json.NewEncoder(stdout)
	parsed, skipped := 0, 0
	diagnosticCounts := make(map[string]int)
	for decoder.Scan() {
		result := decoder.Result()
		if result.Diagnostic != nil {
			skipped++
			diagnosticCounts[result.Diagnostic.Code]++
			continue
		}
		if err := encoder.Encode(newJSONEvent(result.Event)); err != nil {
			fmt.Fprintf(stderr, "goflexlm: write output: %v\n", err)
			return 1
		}
		parsed++
	}
	if skipped > 0 {
		codes := make([]string, 0, len(diagnosticCounts))
		for code := range diagnosticCounts {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		counts := make([]string, 0, len(codes))
		for _, code := range codes {
			counts = append(counts, fmt.Sprintf("%s: %d", code, diagnosticCounts[code]))
		}
		lineWord := "lines"
		if skipped == 1 {
			lineWord = "line"
		}
		if _, err := fmt.Fprintf(stderr, "goflexlm: skipped %d unparseable %s (%s)\n", skipped, lineWord, strings.Join(counts, ", ")); err != nil {
			return 1
		}
	}
	if err := decoder.Err(); err != nil {
		fmt.Fprintf(stderr, "goflexlm: read input: %v\n", err)
		return 1
	}
	if parsed == 0 {
		fmt.Fprintln(stderr, "goflexlm: no parseable events")
		return 1
	}
	return 0
}

type jsonEvent struct {
	Line      int                `json:"line"`
	Raw       string             `json:"raw"`
	Time      string             `json:"time"`
	Timestamp *string            `json:"timestamp,omitempty"`
	Daemon    string             `json:"daemon"`
	Kind      goflexlm.Kind      `json:"kind"`
	Message   string             `json:"message"`
	Activity  *goflexlm.Activity `json:"activity,omitempty"`
}

func newJSONEvent(event *goflexlm.Event) jsonEvent {
	out := jsonEvent{
		Line: event.Line, Raw: event.Raw, Time: event.LogTime.Original,
		Daemon: event.Daemon, Kind: event.Kind, Message: event.Message, Activity: event.Activity,
	}
	if event.LogTime.Time != nil {
		formatted := event.LogTime.Time.Format(time.RFC3339Nano)
		out.Timestamp = &formatted
	}
	return out
}
