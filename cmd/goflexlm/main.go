// Command goflexlm converts a FlexNet Publisher debug log to JSON Lines.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/danofsteel32/goflexlm"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintln(stderr, "usage: goflexlm [FILE|-]")
		return 2
	}

	input := stdin
	var file *os.File
	if len(args) == 1 && args[0] != "-" {
		var err error
		file, err = os.Open(args[0])
		if err != nil {
			fmt.Fprintf(stderr, "goflexlm: %v\n", err)
			return 1
		}
		defer file.Close()
		input = file
	}

	decoder := goflexlm.NewDecoder(input)
	encoder := json.NewEncoder(stdout)
	hadDiagnostics := false
	for decoder.Scan() {
		result := decoder.Result()
		if result.Diagnostic != nil {
			hadDiagnostics = true
			d := result.Diagnostic
			if _, err := fmt.Fprintf(stderr, "line %d: %s: %s\n", d.Line, d.Code, d.Message); err != nil {
				return 1
			}
			continue
		}
		if err := encoder.Encode(newJSONEvent(result.Event)); err != nil {
			fmt.Fprintf(stderr, "goflexlm: write output: %v\n", err)
			return 1
		}
	}
	if err := decoder.Err(); err != nil {
		fmt.Fprintf(stderr, "goflexlm: read input: %v\n", err)
		return 1
	}
	if hadDiagnostics {
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
