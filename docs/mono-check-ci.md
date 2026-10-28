# Publishing `mono check` test results in Jenkins

`mono check --junit DIR` writes one JUnit XML file for each Go or Go TUI project whose test phase runs. The directory is resolved from the command's working directory, created when needed, and must be empty when the command starts. Existing files are never removed. Reporting implies `--no-cache` for every check phase, so a cached test cannot suppress a report. Empty selections produce no XML files.

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
