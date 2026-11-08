# Publishing `mono check` test results in Jenkins

`mono check --junit DIR` writes one JUnit XML file for each Go or Go TUI project whose test phase runs. The directory is resolved from the command's working directory, created when needed, and must be empty when the command starts. Existing files are never removed. Reporting implies `--no-cache` for every check phase, so a cached test cannot suppress a report. Empty selections produce no XML files.

Integration tests are a separate CI step. `mono check` does not select them;
invoke `mono test --integration` explicitly and publish any results using the
test runner's reporting options:

```sh
mono check --junit reports/check
mono test --integration --no-cache
```

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
