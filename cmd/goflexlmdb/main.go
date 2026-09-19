// Command goflexlmdb imports FlexNet activity into SQLite and reports usage.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/danofsteel32/goflexlm"
	store "github.com/danofsteel32/goflexlm/sqlite"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "import":
		return runImport(args[1:], stdin, stdout, stderr)
	case "licenses":
		return runLicenses(args[1:], stdin, stdout, stderr)
	case "rebuild":
		return runRebuild(args[1:], stderr)
	case "report":
		if len(args) < 2 {
			usage(stderr)
			return 2
		}
		return runReport(args[1], args[2:], stdout, stderr)
	default:
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: goflexlmdb import --db DB --pool POOL --stream STREAM FILE...")
	fmt.Fprintln(w, "       goflexlmdb licenses parse [FILE|-]")
	fmt.Fprintln(w, "       goflexlmdb licenses import --db DB --pool POOL --effective-from RFC3339 --timezone AREA/LOCATION FILE")
	fmt.Fprintln(w, "       goflexlmdb rebuild --db DB --pool POOL")
	fmt.Fprintln(w, "       goflexlmdb report capacity|denials|queues --db DB --pool POOL --from RFC3339 --to RFC3339 [--feature FEATURE] [--bucket hour|day|week|month] [--timezone AREA/LOCATION] [--json]")
}

func runLicenses(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "import" {
		return runLicenseImport(args[1:], stdin, stderr, openInput, store.Open)
	}
	if len(args) == 0 || args[0] != "parse" {
		usage(stderr)
		return 2
	}
	if len(args) > 2 {
		usage(stderr)
		return 2
	}
	path := "-"
	if len(args) == 2 {
		path = args[1]
	}
	return runLicenseParse(path, stdin, stdout, stderr, openInput)
}

func runLicenseImport(args []string, stdin io.Reader, stderr io.Writer,
	opener func(string, io.Reader) (io.Reader, func() error, error),
	openDatabase func(context.Context, string, store.OpenOptions) (*store.Store, error)) int {
	set := flags("licenses import", stderr)
	dbPath := set.String("db", "", "SQLite database")
	pool := set.String("pool", "", "license pool")
	effective := set.String("effective-from", "", "snapshot effective instant")
	zone := set.String("timezone", "", "license date timezone")
	if e := set.Parse(args); e != nil {
		set.Usage()
		return 2
	}
	when, e := time.Parse(time.RFC3339Nano, *effective)
	validZone := *zone == "UTC" || strings.Contains(*zone, "/")
	_, zoneError := time.LoadLocation(*zone)
	if strings.TrimSpace(*dbPath) == "" || strings.TrimSpace(*pool) == "" || e != nil || when.IsZero() || !validZone || zoneError != nil || set.NArg() != 1 {
		set.Usage()
		return 2
	}
	path := set.Arg(0)
	file, e := readLicenseInput(path, stdin, opener)
	if e != nil {
		failure(stderr, e)
		return 1
	}
	db, e := openDatabase(context.Background(), *dbPath, store.OpenOptions{})
	if e != nil {
		failure(stderr, e)
		return 1
	}
	e = db.ImportLicenseFile(context.Background(), store.LicenseImportRequest{File: file, Pool: *pool, SourceName: path, EffectiveFrom: when, Timezone: *zone})
	closeErr := db.Close()
	if e != nil {
		failure(stderr, e)
		return 1
	}
	if closeErr != nil {
		failure(stderr, closeErr)
		return 1
	}
	return 0
}

func runLicenseParse(path string, stdin io.Reader, stdout, stderr io.Writer, opener func(string, io.Reader) (io.Reader, func() error, error)) int {
	file, err := readLicenseInput(path, stdin, opener)
	if err != nil {
		failure(stderr, err)
		return 1
	}
	data, err := json.Marshal(file)
	if err != nil {
		failure(stderr, fmt.Errorf("encode license document: %w", err))
		return 1
	}
	data = append(data, '\n')
	n, err := stdout.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		failure(stderr, fmt.Errorf("write license document: %w", err))
		return 1
	}
	return 0
}

func readLicenseInput(path string, stdin io.Reader, opener func(string, io.Reader) (io.Reader, func() error, error)) (goflexlm.LicenseFile, error) {
	reader, closeReader, err := opener(path, stdin)
	if err != nil {
		return goflexlm.LicenseFile{}, fmt.Errorf("open %s: %w", path, err)
	}
	file, err := goflexlm.ParseLicenseFile(reader)
	closeErr := closeReader()
	if err != nil {
		return goflexlm.LicenseFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if closeErr != nil {
		return goflexlm.LicenseFile{}, fmt.Errorf("close %s: %w", path, closeErr)
	}
	return file, nil
}

func flags(name string, stderr io.Writer) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(stderr)
	set.Usage = func() { usage(stderr) }
	return set
}

func runImport(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	set := flags("import", stderr)
	dbPath := set.String("db", "", "SQLite database")
	pool := set.String("pool", "", "license pool")
	stream := set.String("stream", "", "logical log stream")
	if err := set.Parse(args); err != nil || *dbPath == "" || *pool == "" || *stream == "" || len(set.Args()) == 0 {
		if err == nil {
			set.Usage()
		}
		return 2
	}
	db, err := store.Open(context.Background(), *dbPath, store.OpenOptions{})
	if err != nil {
		failure(stderr, err)
		return 1
	}
	defer db.Close()
	status := 0
	for _, path := range set.Args() {
		reader, closeReader, err := openInput(path, stdin)
		if err != nil {
			failure(stderr, err)
			return 1
		}
		result, err := db.Import(context.Background(), store.ImportRequest{
			Reader: reader, Pool: *pool, Stream: *stream, SourceName: path,
			OnDiagnostic: func(diagnostic goflexlm.Diagnostic) error {
				_, writeErr := fmt.Fprintf(stderr, "%s:line %d: %s: %s\n", path, diagnostic.Line, diagnostic.Code, diagnostic.Message)
				return writeErr
			},
		})
		closeErr := closeReader()
		if err != nil {
			failure(stderr, err)
			return 1
		}
		if closeErr != nil {
			failure(stderr, fmt.Errorf("close %s: %w", path, closeErr))
			return 1
		}
		if len(result.DiagnosticCounts) != 0 {
			status = 1
		}
		state := "imported"
		if result.Duplicate {
			state = "duplicate"
		}
		if _, err := fmt.Fprintf(stdout, "%s: %s: %d activities, %d diagnostics, sha256 %s\n",
			path, state, result.ActivityCount, countDiagnostics(result.DiagnosticCounts), result.Digest); err != nil {
			failure(stderr, fmt.Errorf("write import summary: %w", err))
			return 1
		}
	}
	return status
}

func countDiagnostics(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func openInput(path string, stdin io.Reader) (io.Reader, func() error, error) {
	if path == "-" {
		return stdin, func() error { return nil }, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}
	return file, file.Close, nil
}

func runRebuild(args []string, stderr io.Writer) int {
	set := flags("rebuild", stderr)
	dbPath := set.String("db", "", "SQLite database")
	pool := set.String("pool", "", "license pool")
	if err := set.Parse(args); err != nil || *dbPath == "" || *pool == "" || len(set.Args()) != 0 {
		if err == nil {
			set.Usage()
		}
		return 2
	}
	db, err := store.Open(context.Background(), *dbPath, store.OpenOptions{})
	if err != nil {
		failure(stderr, err)
		return 1
	}
	defer db.Close()
	if err := db.Rebuild(context.Background(), *pool); err != nil {
		failure(stderr, err)
		return 1
	}
	return 0
}

func runReport(kind string, args []string, stdout, stderr io.Writer) int {
	if kind != "capacity" && kind != "denials" && kind != "queues" {
		usage(stderr)
		return 2
	}
	set := flags("report "+kind, stderr)
	dbPath := set.String("db", "", "SQLite database")
	pool := set.String("pool", "", "license pool")
	fromText := set.String("from", "", "range start")
	toText := set.String("to", "", "range end")
	feature := set.String("feature", "", "feature")
	bucketText := set.String("bucket", "", "calendar bucket")
	timezone := set.String("timezone", "UTC", "IANA timezone")
	jsonOutput := set.Bool("json", false, "write JSON")
	if err := set.Parse(args); err != nil || *dbPath == "" || *pool == "" || *fromText == "" || *toText == "" || len(set.Args()) != 0 {
		if err == nil {
			set.Usage()
		}
		return 2
	}
	from, err := time.Parse(time.RFC3339Nano, *fromText)
	if err != nil {
		fmt.Fprintln(stderr, "goflexlmdb: --from must be RFC3339")
		return 2
	}
	to, err := time.Parse(time.RFC3339Nano, *toText)
	if err != nil || !from.Before(to) {
		fmt.Fprintln(stderr, "goflexlmdb: --to must be RFC3339 and after --from")
		return 2
	}
	location, err := time.LoadLocation(*timezone)
	if err != nil {
		fmt.Fprintf(stderr, "goflexlmdb: invalid --timezone: %v\n", err)
		return 2
	}
	bucket := store.CalendarBucket(*bucketText)
	if bucket != "" && bucket != store.BucketHour && bucket != store.BucketDay && bucket != store.BucketWeek && bucket != store.BucketMonth {
		fmt.Fprintln(stderr, "goflexlmdb: --bucket must be hour, day, week, or month")
		return 2
	}
	db, err := store.Open(context.Background(), *dbPath, store.OpenOptions{})
	if err != nil {
		failure(stderr, err)
		return 1
	}
	defer db.Close()
	query := store.AnalyticsQuery{Pool: *pool, Feature: *feature, From: from, To: to, Bucket: bucket, Timezone: location}
	var report any
	switch kind {
	case "capacity":
		report, err = db.Capacity(context.Background(), query)
	case "denials":
		report, err = db.Denials(context.Background(), query)
	case "queues":
		report, err = db.Queueing(context.Background(), query)
	}
	if err != nil {
		failure(stderr, err)
		return 1
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(report); err != nil {
			failure(stderr, fmt.Errorf("write report: %w", err))
			return 1
		}
		return 0
	}
	if err := writeTable(stdout, report); err != nil {
		failure(stderr, fmt.Errorf("write report: %w", err))
		return 1
	}
	return 0
}

func writeTable(output io.Writer, report any) error {
	w := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	switch value := report.(type) {
	case store.CapacityReport:
		fmt.Fprintln(w, "VENDOR\tFEATURE\tFROM\tTO\tPURCHASED\tLOWER PEAK\tUPPER PEAK")
		for _, bucket := range value.Buckets {
			purchased := "unknown"
			if bucket.Uncounted {
				purchased = "uncounted"
			} else if bucket.Purchased != nil {
				purchased = strconv.Itoa(*bucket.Purchased)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%d\n", bucket.Vendor, bucket.Feature, formatTime(bucket.From), formatTime(bucket.To), purchased, bucket.LowerPeak, bucket.UpperPeak)
		}
	case store.DenialReport:
		fmt.Fprintln(w, "FEATURE\tREASON\tERROR\tEVENTS\tLICENSES")
		for _, bucket := range value.Buckets {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\n", bucket.Feature, bucket.Reason, bucket.ErrorCode, bucket.Events, bucket.Licenses)
		}
	case store.QueueReport:
		fmt.Fprintln(w, "FEATURE\tQUEUED EVENTS\tQUEUED LICENSES\tMAX DEPTH\tCOMPLETED WAITS")
		for _, bucket := range value.Buckets {
			fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\n", bucket.Feature, bucket.QueuedEvents, bucket.QueuedLicenses, bucket.MaximumDepth, bucket.CompletedWaits)
		}
	default:
		return errors.New("unsupported report type")
	}
	return w.Flush()
}

func formatTime(value time.Time) string { return value.Format(time.RFC3339) }

func failure(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "goflexlmdb: %v\n", err)
}
