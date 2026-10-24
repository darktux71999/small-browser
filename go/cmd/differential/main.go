// Command differential is the S1 acceptance runner for the dual-engine Go
// migration: for every fixture in the shared corpus (test/data/*.html) it
// runs the Rust oracle and the Go candidate with --dump and --png, compares
// the byte streams, and prints a per-fixture table.
//
// Usage:
//
//	differential --oracle <path> --candidate <path> --corpus <dir> --report text|json
//
// When either engine binary does not exist yet the runner reports
// "<role>: not built" cleanly and exits 0 (nothing to compare). Exit 1
// signals unexpected diffs; exit 2 signals a usage or configuration error.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// engineTimeout bounds a single engine invocation so a hung engine cannot
// stall the whole corpus run.
const engineTimeout = 60 * time.Second

// Status values for a fixture comparison.
const (
	statusIdentical = "identical"
	statusDiff      = "DIFF"
	statusSkip      = "skip"
)

// FixtureResult is the comparison outcome for one corpus fixture.
type FixtureResult struct {
	Fixture string   `json:"fixture"`
	Status  string   `json:"status"`
	Diffs   []string `json:"diffs,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// Summary counts the comparison outcomes.
type Summary struct {
	Total     int `json:"total"`
	Identical int `json:"identical"`
	Diff      int `json:"diff"`
	Skipped   int `json:"skipped"`
}

// Report is the machine-readable form of a differential run.
type Report struct {
	Oracle    string          `json:"oracle"`
	Candidate string          `json:"candidate"`
	Corpus    string          `json:"corpus"`
	Results   []FixtureResult `json:"results"`
	Summary   Summary         `json:"summary"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run executes the differential comparison and renders the report to out.
// It returns the process exit code: 0 when every fixture is identical (or
// there is nothing to compare because an engine is not built yet), 1 on
// unexpected diffs, 2 on usage or configuration errors.
func run(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("differential", flag.ContinueOnError)
	oracle := fs.String("oracle", "../target/debug/browser-lite", "path to the Rust oracle binary")
	candidate := fs.String("candidate", "", "path to the Go candidate binary")
	corpus := fs.String("corpus", "../test/data", "directory with *.html fixtures")
	reportFmt := fs.String("report", "text", "report format: text or json")
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *reportFmt != "text" && *reportFmt != "json" {
		fmt.Fprintf(out, "differential: invalid --report %q: want text or json\n", *reportFmt)
		return 2
	}
	entries, err := os.ReadDir(*corpus)
	if err != nil {
		fmt.Fprintf(out, "differential: corpus %s: %v\n", *corpus, err)
		return 2
	}
	var fixtures []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".html") {
			fixtures = append(fixtures, e.Name())
		}
	}
	sort.Strings(fixtures)

	report := Report{Oracle: *oracle, Candidate: *candidate, Corpus: *corpus}
	oracleOK := engineExists(*oracle)
	candidateOK := engineExists(*candidate)
	if !oracleOK || !candidateOK {
		for _, f := range fixtures {
			report.Results = append(report.Results, FixtureResult{Fixture: f, Status: statusSkip, Note: "engine not built"})
		}
		report.Summary = Summary{Total: len(fixtures), Skipped: len(fixtures)}
		renderReport(out, *reportFmt, &report, *oracle, *candidate, oracleOK, candidateOK)
		return 0
	}

	scratch, err := os.MkdirTemp("", "differential")
	if err != nil {
		fmt.Fprintf(out, "differential: scratch dir: %v\n", err)
		return 2
	}
	defer os.RemoveAll(scratch)

	for _, f := range fixtures {
		path := filepath.Join(*corpus, f)
		report.Results = append(report.Results, compareFixture(*oracle, *candidate, f, path, scratch))
	}
	for _, r := range report.Results {
		switch r.Status {
		case statusIdentical:
			report.Summary.Identical++
		case statusDiff:
			report.Summary.Diff++
		default:
			report.Summary.Skipped++
		}
	}
	report.Summary.Total = len(report.Results)
	renderReport(out, *reportFmt, &report, *oracle, *candidate, true, true)
	if report.Summary.Diff > 0 {
		return 1
	}
	return 0
}

func engineExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Mode()&0o111 != 0
}

// engineRun captures one engine invocation's stdout, stderr, and exit code
// (0 on success; -1 when the process never reported a status, such as a
// timeout or a missing binary).
type engineRun struct {
	stdout []byte
	stderr []byte
	exit   int
	err    error
}

func invoke(engine string, args ...string) engineRun {
	ctx, cancel := context.WithTimeout(context.Background(), engineTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, engine, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	r := engineRun{stdout: stdout.Bytes(), stderr: stderr.Bytes(), err: err}
	switch {
	case err == nil:
		r.exit = 0
	default:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			r.exit = exitErr.ExitCode()
		} else {
			r.exit = -1
		}
	}
	return r
}

// compareFixture runs both engines on one fixture twice: `--dump <file>` for
// the DOM/layout dump streams, and `--dump --png <out> <file>` for the PNG
// bytes. The png run's "wrote <path>" line embeds the scratch output path,
// so stdout is normalized before comparison. Engines that fail the same way
// (same exit code and same bytes on every compared stream) count as
// identical: the observable behavior matches.
func compareFixture(oracle, candidate, name, path, scratch string) FixtureResult {
	res := FixtureResult{Fixture: name, Status: statusIdentical}
	var notes []string

	dumpOracle := invoke(oracle, "--dump", path)
	dumpCandidate := invoke(candidate, "--dump", path)
	if dumpOracle.exit != dumpCandidate.exit {
		res.Diffs = append(res.Diffs, "dump-exit-code")
	}
	if !bytes.Equal(dumpOracle.stdout, dumpCandidate.stdout) {
		res.Diffs = append(res.Diffs, "dump-stdout")
	}
	if !bytes.Equal(dumpOracle.stderr, dumpCandidate.stderr) {
		res.Diffs = append(res.Diffs, "dump-stderr")
	}
	if dumpOracle.err != nil || dumpCandidate.err != nil {
		notes = append(notes, engineErrNote("dump", dumpOracle.err, dumpCandidate.err))
	}

	oraclePNG := filepath.Join(scratch, "oracle-"+name+".png")
	candidatePNG := filepath.Join(scratch, "candidate-"+name+".png")
	pngOracle := invoke(oracle, "--dump", "--png", oraclePNG, path)
	pngCandidate := invoke(candidate, "--dump", "--png", candidatePNG, path)
	if pngOracle.exit != pngCandidate.exit {
		res.Diffs = append(res.Diffs, "png-exit-code")
	}
	if !bytes.Equal(normalizeWrote(pngOracle.stdout, oraclePNG), normalizeWrote(pngCandidate.stdout, candidatePNG)) {
		res.Diffs = append(res.Diffs, "png-stdout")
	}
	if !bytes.Equal(pngOracle.stderr, pngCandidate.stderr) {
		res.Diffs = append(res.Diffs, "png-stderr")
	}
	if pngOracle.err != nil || pngCandidate.err != nil {
		notes = append(notes, engineErrNote("png", pngOracle.err, pngCandidate.err))
	}
	if !equalPNGFiles(oraclePNG, candidatePNG) {
		res.Diffs = append(res.Diffs, "png")
	}

	if len(res.Diffs) > 0 {
		res.Status = statusDiff
		res.Note = strings.Join(notes, "; ")
	}
	return res
}

// equalPNGFiles reports whether both engines wrote byte-identical PNGs. Two
// missing files (both engines failed before painting) count as equal; a PNG
// on exactly one side, or unreadable files, count as different.
func equalPNGFiles(oraclePNG, candidatePNG string) bool {
	oracleBytes, oErr := os.ReadFile(oraclePNG)
	candidateBytes, cErr := os.ReadFile(candidatePNG)
	switch {
	case oErr == nil && cErr == nil:
		return bytes.Equal(oracleBytes, candidateBytes)
	case oErr != nil && cErr != nil:
		return os.IsNotExist(oErr) && os.IsNotExist(cErr)
	default:
		return false
	}
}

func engineErrNote(phase string, oracleErr, candidateErr error) string {
	switch {
	case oracleErr != nil && candidateErr != nil:
		return fmt.Sprintf("%s run failed on both engines (oracle: %v; candidate: %v)", phase, oracleErr, candidateErr)
	case oracleErr != nil:
		return fmt.Sprintf("%s run failed on oracle: %v", phase, oracleErr)
	default:
		return fmt.Sprintf("%s run failed on candidate: %v", phase, candidateErr)
	}
}

// normalizeWrote replaces the scratch output path in the engine's
// "wrote <path> (WxH)" line with a placeholder so runs writing to different
// scratch files remain comparable.
func normalizeWrote(out []byte, pngPath string) []byte {
	return bytes.ReplaceAll(out, []byte(pngPath), []byte("<png>"))
}

func renderReport(out io.Writer, format string, report *Report, oracle, candidate string, oracleOK, candidateOK bool) {
	if format == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return
	}
	fmt.Fprintf(out, "oracle: %s\n", describeEngine(oracle, oracleOK))
	fmt.Fprintf(out, "candidate: %s\n", describeEngine(candidate, candidateOK))
	fmt.Fprintf(out, "corpus: %s (%d fixtures)\n", report.Corpus, len(report.Results))
	if !oracleOK || !candidateOK {
		fmt.Fprintf(out, "status: no engines to compare; build the missing engine first\n")
		return
	}
	fmt.Fprintf(out, "\n%-40s %-10s %s\n", "fixture", "status", "details")
	for _, r := range report.Results {
		details := "-"
		if len(r.Diffs) > 0 {
			details = strings.Join(r.Diffs, " ")
		} else if r.Note != "" {
			details = r.Note
		}
		fmt.Fprintf(out, "%-40s %-10s %s\n", r.Fixture, r.Status, details)
	}
	fmt.Fprintf(out, "\n%d fixtures: %d identical, %d DIFF, %d skipped\n",
		report.Summary.Total, report.Summary.Identical, report.Summary.Diff, report.Summary.Skipped)
}

func describeEngine(path string, ok bool) string {
	if path == "" {
		return "not built (no --candidate given)"
	}
	if !ok {
		return path + " (not built)"
	}
	return path
}
