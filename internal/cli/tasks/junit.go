package tasks

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type goTestEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
	Elapsed float64   `json:"Elapsed"`
	Output  string    `json:"Output"`
}

type junitReport struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Skipped  int             `xml:"skipped,attr"`
	Time     float64         `xml:"time,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type junitSkipped struct{}

type goTestCaseState struct {
	name, output string
	duration     float64
	terminal     bool
	failure      bool
	skipped      bool
}

func writeGoTestJUnit(dir string, svc Service, events []byte) error {
	report, err := convertGoTestJSON(events)
	if err != nil {
		return fmt.Errorf("converting Go test events for %s: %w", svc.Name, err)
	}
	data, err := xml.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding JUnit report for %s: %w", svc.Name, err)
	}
	data = append([]byte(xml.Header), data...)
	identity := svc.Name + "\x00" + svc.Path
	sum := sha256.Sum256([]byte(identity))
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, svc.Name)
	if name == "" {
		name = "go-project"
	}
	path := filepath.Join(dir, name+"-"+hex.EncodeToString(sum[:6])+".xml")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("creating JUnit report %s: %w", path, err)
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("writing JUnit report %s: %w", path, errors.Join(writeErr, closeErr))
	}
	return nil
}

func convertGoTestJSON(data []byte) (junitReport, error) {
	packages := map[string]map[string]*goTestCaseState{}
	packageOutput := map[string]string{}
	packageFailed := map[string]bool{}
	packageDone := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var event goTestEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return junitReport{}, fmt.Errorf("invalid JSON event on line %d: %w", line, err)
		}
		if event.Package == "" {
			return junitReport{}, fmt.Errorf("event on line %d has no package", line)
		}
		if packages[event.Package] == nil {
			packages[event.Package] = map[string]*goTestCaseState{}
		}
		if event.Test == "" {
			packageOutput[event.Package] += event.Output
			if event.Action == "fail" {
				packageFailed[event.Package] = true
			}
			if event.Action == "pass" || event.Action == "fail" || event.Action == "skip" {
				packageDone[event.Package] = true
			}
			continue
		}
		state := packages[event.Package][event.Test]
		if state == nil {
			state = &goTestCaseState{name: event.Test}
			packages[event.Package][event.Test] = state
		}
		switch event.Action {
		case "output":
			state.output += event.Output
		case "pass", "fail", "skip":
			state.terminal = true
			state.duration = event.Elapsed
			state.failure = event.Action == "fail"
			state.skipped = event.Action == "skip"
		}
	}
	if err := scanner.Err(); err != nil {
		return junitReport{}, fmt.Errorf("reading Go test events: %w", err)
	}
	if line == 0 {
		return junitReport{}, fmt.Errorf("Go test event stream is empty")
	}
	for pkg, tests := range packages {
		for name, state := range tests {
			if !state.terminal {
				return junitReport{}, fmt.Errorf("incomplete event stream: %s/%s has no terminal event", pkg, name)
			}
		}
	}
	report := junitReport{}
	packageNames := make([]string, 0, len(packages))
	for pkg := range packages {
		packageNames = append(packageNames, pkg)
	}
	sort.Strings(packageNames)
	for _, pkg := range packageNames {
		tests := packages[pkg]
		suite := junitTestSuite{Name: pkg}
		testNames := make([]string, 0, len(tests))
		for name := range tests {
			testNames = append(testNames, name)
		}
		sort.Strings(testNames)
		for _, testName := range testNames {
			state := tests[testName]
			caseResult := junitTestCase{Name: state.name, Classname: pkg, Time: state.duration}
			switch {
			case state.failure:
				suite.Failures++
			case state.skipped:
				suite.Skipped++
			}
			if state.failure {
				caseResult.Failure = &junitFailure{Message: "Go test failed", Body: state.output}
			} else if state.skipped {
				caseResult.Skipped = &junitSkipped{}
			}
			if state.output != "" && !state.failure {
				caseResult.SystemOut = state.output
			}
			suite.Cases = append(suite.Cases, caseResult)
			suite.Time += state.duration
		}
		if len(tests) == 0 && packageFailed[pkg] {
			suite.Cases = append(suite.Cases, junitTestCase{Name: "package build or setup", Classname: pkg, Failure: &junitFailure{Message: "Go package failed", Body: packageOutput[pkg]}})
			suite.Failures++
		}
		if !packageDone[pkg] {
			return junitReport{}, fmt.Errorf("incomplete event stream: package %s has no terminal event", pkg)
		}
		suite.Tests = len(suite.Cases)
		report.Suites = append(report.Suites, suite)
	}
	if len(report.Suites) == 0 {
		return junitReport{}, fmt.Errorf("Go test event stream contains no package results")
	}
	return report, nil
}
