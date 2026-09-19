// Command goflexlmdb imports FlexNet activity into SQLite and reports usage.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
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
	case "entitlements":
		if len(args) < 2 || args[1] != "replace" {
			usage(stderr)
			return 2
		}
		return runEntitlements(args[2:], stdin, stderr)
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
	fmt.Fprintln(w, "       goflexlmdb entitlements replace --db DB --pool POOL --feature FEATURE FILE.csv")
	fmt.Fprintln(w, "       goflexlmdb rebuild --db DB --pool POOL")
	fmt.Fprintln(w, "       goflexlmdb report capacity|denials|queues --db DB --pool POOL --from RFC3339 --to RFC3339 [--feature FEATURE] [--bucket hour|day|week|month] [--timezone AREA/LOCATION] [--json]")
}

func runLicenses(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
	reader, closeReader, err := openInput(path, stdin)
	if err != nil {
		failure(stderr, err)
		return 1
	}
	file, err := goflexlm.ParseLicenseFile(reader)
	closeErr := closeReader()
	if err != nil {
		failure(stderr, fmt.Errorf("parse %s: %w", path, err))
		return 1
	}
	if closeErr != nil {
		failure(stderr, fmt.Errorf("close %s: %w", path, closeErr))
		return 1
	}
	data, err := json.Marshal(file)
	if err != nil {
		failure(stderr, fmt.Errorf("encode license document: %w", err))
		return 1
	}
	if _, err := fmt.Fprintln(stdout, string(data)); err != nil {
		failure(stderr, fmt.Errorf("write license document: %w", err))
		return 1
	}
	return 0
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

func runEntitlements(args []string, stdin io.Reader, stderr io.Writer) int {
	set := flags("entitlements replace", stderr)
	dbPath := set.String("db", "", "SQLite database")
	pool := set.String("pool", "", "license pool")
	feature := set.String("feature", "", "feature")
	if err := set.Parse(args); err != nil || *dbPath == "" || *pool == "" || *feature == "" || len(set.Args()) != 1 {
		if err == nil {
			set.Usage()
		}
		return 2
	}
	reader, closeReader, err := openInput(set.Args()[0], stdin)
	if err != nil {
		failure(stderr, err)
		return 1
	}
	defer closeReader()
	entitlements, err := readEntitlements(reader)
	if err != nil {
		failure(stderr, err)
		return 1
	}
	db, err := store.Open(context.Background(), *dbPath, store.OpenOptions{})
	if err != nil {
		failure(stderr, err)
		return 1
	}
	defer db.Close()
	if err := db.ReplaceEntitlements(context.Background(), *pool, *feature, entitlements); err != nil {
		failure(stderr, err)
		return 1
	}
	return 0
}

func readEntitlements(reader io.Reader) ([]store.Entitlement, error) {
	rows := csv.NewReader(reader)
	header, err := rows.Read()
	if err != nil {
		return nil, fmt.Errorf("read entitlement CSV header: %w", err)
	}
	if len(header) != 2 || header[0] != "effective_from" || header[1] != "licenses" {
		return nil, errors.New("entitlement CSV must have exact columns effective_from,licenses")
	}
	var result []store.Entitlement
	for line := 2; ; line++ {
		record, err := rows.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read entitlement CSV line %d: %w", line, err)
		}
		instant, err := time.Parse(time.RFC3339Nano, record[0])
		if err != nil {
			return nil, fmt.Errorf("entitlement CSV line %d: invalid RFC3339 effective_from", line)
		}
		licenses, err := strconv.Atoi(record[1])
		if err != nil || licenses < 0 {
			return nil, fmt.Errorf("entitlement CSV line %d: licenses must be a non-negative integer", line)
		}
		result = append(result, store.Entitlement{EffectiveFrom: instant, Licenses: licenses})
	}
	return result, nil
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
