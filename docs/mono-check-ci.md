# Publishing `mono check` test results in Jenkins

`mono check --junit DIR` writes one JUnit XML file for each Go or Go TUI project whose test phase runs. The directory is resolved from the command's working directory, created when needed, and must be empty when the command starts. Existing files are never removed. Reporting implies `--no-cache` for every check phase, so a cached test cannot suppress a report. Empty selections produce no XML files.

Integration tests are a separate CI step. `mono check` does not select them;
invoke `mono test --integration` explicitly and publish any results using the
test runner's reporting options:

```sh
mono check --junit reports/check
mono test --integration --no-cache
```

Focused runs select Go packages with repeatable `--package` and top-level tests
with `--run`. `--list` reports sorted service/package/test candidates without
running tests. Go regex slash components are passed through for subtests, but
listing cannot enumerate dynamic subtests. Selectors apply to runnable
direct service nodes. Empty direct selections fail before tests; dependencies
remain in the graph for ordering, while unmatched dependency test nodes are
skipped. A focused run rejects runnable non-Go nodes. `--go-concurrency N`
controls Go package parallelism separately from Mono task `--concurrency` and
conflicts with `GOFLAGS` package parallel settings when explicitly supplied.

Repositories can require explicit intent:

```yaml
integration:
  requireFocus: true
```

With this opt-in, specify `--package`, `--run`, or `--full-suite`; service
selection remains independent, and `--full-suite` does not mean all services.
An absent or false setting preserves broad legacy behavior.

Integration test children can read `test.env` from the invocation directory and
their service directory. Service values override invocation defaults, and
inherited variables override both, including empty values. Each file is parsed
independently; missing files are optional and malformed or unreadable files fail
before cache lookup or process launch. This cascade is limited to integration
test children; it does not load development `.env` files or apply to installs.

The integration cache key includes the profile and a digest of the complete
effective child environment, while keeping values out of cache metadata. Source
hashing may also invalidate entries when files change. External services and
their state are not part of the cache identity, so use `--no-cache` when a fresh
integration run is required.

An optional service `integrationPreflight` command runs after selection and
before cache lookup or test execution, including when a cache entry exists. It
uses Mono's shell-free allowlist and the same effective environment snapshot
and working directory as the test. Failure or cancellation blocks the cache
hit and test. The command is part of cache identity; external state still
requires `--no-cache` when it can affect results.

Proposed Flock migration (the checker is consumer-owned; this does not claim
that a Flock configuration was verified):

```yaml
integration:
  requireFocus: true
services:
  - name: flock
    integrationPreflight: node scripts/check-integration-env.js
```

```sh
mono test flock --integration --package './internal/store/...' --run '^TestStore$' --go-concurrency 2
```

The Flock wrapper can delegate selection and dotenv loading to Mono, leaving
resource checks in the application checker without a per-package `TestMain`
selection guard. `node` is allowlisted; the consumer supplies the script.

Jenkins should publish in an `always` post condition so reports from failed checks remain visible:

```groovy
pipeline {
  stages {
    stage('Check') {
      steps {
        sh 'mono check --junit reports'
      }
    }
  }
  post {
    always {
      junit testResults: 'reports/*.xml', allowEmptyResults: false
    }
  }
}
```

For a job where no Go test task is selected, choose an explicit policy for the empty result set. To accept it, set `allowEmptyResults: true`; otherwise Jenkins will report missing results. The `mono check` exit status remains nonzero when a test or report generation fails.
