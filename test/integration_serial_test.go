package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	terragruntinfo "github.com/gruntwork-io/terragrunt/cli/commands/terragrunt-info"
	"github.com/gruntwork-io/terragrunt/config"
	"github.com/gruntwork-io/terragrunt/util"
)

// NOTE: We don't run these tests in parallel because it modifies the environment variable, so it can affect other tests

func TestTerragruntDownloadDir(t *testing.T) {
	cleanupTerraformFolder(t, testFixtureLocalRelativeDownloadPath)
	tmpEnvPath := copyEnvironment(t, TEST_FIXTURE_GET_OUTPUT)

	/* we have 2 terragrunt dirs here. One of them doesn't set the download_dir in the config,
	the other one does. Here we'll be checking for precedence, and if the download_dir is set
	according to the specified settings
	*/
	testCases := []struct {
		name                 string
		rootPath             string
		downloadDirEnv       string // download dir set as an env var
		downloadDirFlag      string // download dir set as a flag
		downloadDirReference string // the expected result
	}{
		{
			"download dir not set",
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "not-set"),
			"", // env
			"", // flag
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "not-set", TERRAGRUNT_CACHE),
		},
		{
			"download dir set in config",
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config"),
			"", // env
			"", // flag
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config", ".download"),
		},
		{
			"download dir set in config and in env var",
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config"),
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config", ".env-var"),
			"", // flag
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config", ".env-var"),
		},
		{
			"download dir set in config and as a flag",
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config"),
			"", // env
			fmt.Sprintf("--terragrunt-download-dir %s", util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config", ".flag-download")),
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config", ".flag-download"),
		},
		{
			"download dir set in config env and as a flag",
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config"),
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config", ".env-var"),
			fmt.Sprintf("--terragrunt-download-dir %s", util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config", ".flag-download")),
			util.JoinPath(tmpEnvPath, TEST_FIXTURE_GET_OUTPUT, "download-dir", "in-config", ".flag-download"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			if testCase.downloadDirEnv != "" {
				t.Setenv("TERRAGRUNT_DOWNLOAD", testCase.downloadDirEnv)
			} else {
				// Clear the variable if it's not set. This is clearing the variable in case the variable is set outside the test process.
				require.NoError(t, os.Unsetenv("TERRAGRUNT_DOWNLOAD"))
			}
			stdout := bytes.Buffer{}
			stderr := bytes.Buffer{}
			err := runTerragruntCommand(t, fmt.Sprintf("terragrunt terragrunt-info %s --terragrunt-non-interactive --terragrunt-working-dir %s", testCase.downloadDirFlag, testCase.rootPath), &stdout, &stderr)
			logBufferContentsLineByLine(t, stdout, "stdout")
			logBufferContentsLineByLine(t, stderr, "stderr")
			require.NoError(t, err)

			var dat terragruntinfo.TerragruntInfoGroup
			err_unmarshal := json.Unmarshal(stdout.Bytes(), &dat)
			assert.NoError(t, err_unmarshal)
			// compare the results
			assert.Equal(t, testCase.downloadDirReference, dat.DownloadDir)
		})
	}

}

func TestTerragruntCorrectlyMirrorsTerraformGCPAuth(t *testing.T) {
	// We need to ensure Terragrunt works correctly when GOOGLE_CREDENTIALS are specified.
	// There is no true way to properly unset env vars from the environment, but we still try
	// to unset the CI credentials during this test.
	defaultCreds := os.Getenv("GCLOUD_SERVICE_KEY")
	defer os.Setenv("GCLOUD_SERVICE_KEY", defaultCreds)
	os.Unsetenv("GCLOUD_SERVICE_KEY")
	os.Setenv("GOOGLE_CREDENTIALS", defaultCreds)

	cleanupTerraformFolder(t, TEST_FIXTURE_GCS_PATH)

	// We need a project to create the bucket in, so we pull one from the recommended environment variable.
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	gcsBucketName := fmt.Sprintf("terragrunt-test-bucket-%s", strings.ToLower(uniqueId()))

	defer deleteGCSBucket(t, gcsBucketName)

	tmpTerragruntGCSConfigPath := createTmpTerragruntGCSConfig(t, TEST_FIXTURE_GCS_PATH, project, TERRAFORM_REMOTE_STATE_GCP_REGION, gcsBucketName, config.DefaultTerragruntConfigPath)
	runTerragrunt(t, fmt.Sprintf("terragrunt apply -auto-approve --terragrunt-non-interactive --terragrunt-config %s --terragrunt-working-dir %s", tmpTerragruntGCSConfigPath, TEST_FIXTURE_GCS_PATH))

	var expectedGCSLabels = map[string]string{
		"owner": "terragrunt_test",
		"name":  "terraform_state_storage"}
	validateGCSBucketExistsAndIsLabeled(t, TERRAFORM_REMOTE_STATE_GCP_REGION, gcsBucketName, expectedGCSLabels)
}

func TestExtraArguments(t *testing.T) {
	out := new(bytes.Buffer)
	runTerragruntRedirectOutput(t, fmt.Sprintf("terragrunt apply -auto-approve --terragrunt-non-interactive --terragrunt-working-dir %s", TEST_FIXTURE_EXTRA_ARGS_PATH), out, os.Stderr)
	t.Log(out.String())
	assert.Contains(t, out.String(), "Hello, World from dev!")
}

func TestExtraArgumentsWithEnv(t *testing.T) {
	out := new(bytes.Buffer)
	t.Setenv("TF_VAR_env", "prod")
	runTerragruntRedirectOutput(t, fmt.Sprintf("terragrunt apply -auto-approve --terragrunt-non-interactive --terragrunt-working-dir %s", TEST_FIXTURE_EXTRA_ARGS_PATH), out, os.Stderr)
	t.Log(out.String())
	assert.Contains(t, out.String(), "Hello, World!")
}

func TestExtraArgumentsWithEnvVarBlock(t *testing.T) {
	out := new(bytes.Buffer)
	runTerragruntRedirectOutput(t, fmt.Sprintf("terragrunt apply -auto-approve --terragrunt-non-interactive --terragrunt-working-dir %s", TEST_FIXTURE_ENV_VARS_BLOCK_PATH), out, os.Stderr)
	t.Log(out.String())
	assert.Contains(t, out.String(), "I'm set in extra_arguments env_vars")
}

func TestExtraArgumentsWithRegion(t *testing.T) {
	out := new(bytes.Buffer)
	t.Setenv("TF_VAR_region", "us-west-2")
	runTerragruntRedirectOutput(t, fmt.Sprintf("terragrunt apply -auto-approve --terragrunt-non-interactive --terragrunt-working-dir %s", TEST_FIXTURE_EXTRA_ARGS_PATH), out, os.Stderr)
	t.Log(out.String())
	assert.Contains(t, out.String(), "Hello, World from Oregon!")
}

func TestPreserveEnvVarApplyAll(t *testing.T) {
	t.Setenv("TF_VAR_seed", "from the env")

	cleanupTerraformFolder(t, TEST_FIXTURE_REGRESSIONS)
	tmpEnvPath := copyEnvironment(t, TEST_FIXTURE_REGRESSIONS)
	rootPath := util.JoinPath(tmpEnvPath, TEST_FIXTURE_REGRESSIONS, "apply-all-envvar")

	stdout := bytes.Buffer{}
	runTerragruntRedirectOutput(t, fmt.Sprintf("terragrunt apply-all -auto-approve --terragrunt-non-interactive --terragrunt-working-dir %s", rootPath), &stdout, os.Stderr)
	t.Log(stdout.String())

	// Check the output of each child module to make sure the inputs were overridden by the env var
	requireEnvVarModule := util.JoinPath(rootPath, "require-envvar")
	noRequireEnvVarModule := util.JoinPath(rootPath, "no-require-envvar")
	for _, mod := range []string{requireEnvVarModule, noRequireEnvVarModule} {
		stdout := bytes.Buffer{}
		err := runTerragruntCommand(t, fmt.Sprintf("terragrunt output text -no-color --terragrunt-non-interactive --terragrunt-working-dir %s", mod), &stdout, os.Stderr)
		require.NoError(t, err)
		assert.Contains(t, stdout.String(), "Hello from the env")
	}
}

func TestPriorityOrderOfArgument(t *testing.T) {
	out := new(bytes.Buffer)
	injectedValue := "Injected-directly-by-argument"
	runTerragruntRedirectOutput(t, fmt.Sprintf("terragrunt apply -auto-approve -var extra_var=%s --terragrunt-non-interactive --terragrunt-working-dir %s", injectedValue, TEST_FIXTURE_EXTRA_ARGS_PATH), out, os.Stderr)
	t.Log(out.String())
	// And the result value for test should be the injected variable since the injected arguments are injected before the suplied parameters,
	// so our override of extra_var should be the last argument.
	assert.Contains(t, out.String(), fmt.Sprintf(`test = "%s"`, injectedValue))
}

func TestTerragruntValidateInputsWithEnvVar(t *testing.T) {
	t.Setenv("TF_VAR_input", "from the env")

	moduleDir := filepath.Join("fixture-validate-inputs", "fail-no-inputs")
	runTerragruntValidateInputs(t, moduleDir, nil, true)
}

func TestTerragruntValidateInputsWithUnusedEnvVar(t *testing.T) {
	t.Setenv("TF_VAR_unused", "from the env")

	moduleDir := filepath.Join("fixture-validate-inputs", "success-inputs-only")
	args := []string{"--terragrunt-strict-validate"}
	runTerragruntValidateInputs(t, moduleDir, args, false)
}

func TestTerragruntSourceMapEnvArg(t *testing.T) {
	fixtureSourceMapPath := "fixture-source-map"
	cleanupTerraformFolder(t, fixtureSourceMapPath)
	tmpEnvPath := copyEnvironment(t, fixtureSourceMapPath)
	rootPath := filepath.Join(tmpEnvPath, fixtureSourceMapPath)

	t.Setenv(
		"TERRAGRUNT_SOURCE_MAP",
		strings.Join(
			[]string{
				fmt.Sprintf("git::ssh://git@github.com/gruntwork-io/i-dont-exist.git=%s", tmpEnvPath),
				fmt.Sprintf("git::ssh://git@github.com/gruntwork-io/another-dont-exist.git=%s", tmpEnvPath),
			},
			",",
		),
	)
	tgPath := filepath.Join(rootPath, "multiple-match")
	tgArgs := fmt.Sprintf("terragrunt run-all apply -auto-approve --terragrunt-log-level debug --terragrunt-non-interactive --terragrunt-working-dir %s", tgPath)
	runTerragrunt(t, tgArgs)
}

func TestTerragruntLogLevelEnvVarOverridesDefault(t *testing.T) {
	// NOTE: this matches logLevelEnvVar const in util/logger.go
	t.Setenv("TERRAGRUNT_LOG_LEVEL", "debug")

	cleanupTerraformFolder(t, TEST_FIXTURE_INPUTS)
	tmpEnvPath := copyEnvironment(t, ".")
	rootPath := util.JoinPath(tmpEnvPath, TEST_FIXTURE_INPUTS)

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	require.NoError(
		t,
		runTerragruntCommand(t, fmt.Sprintf("terragrunt validate --terragrunt-non-interactive --terragrunt-working-dir %s", rootPath), &stdout, &stderr),
	)
	output := stderr.String()
	assert.Contains(t, output, "level=debug")
}

func TestTerragruntLogLevelEnvVarUnparsableLogsErrorButContinues(t *testing.T) {
	// NOTE: this matches logLevelEnvVar const in util/logger.go
	t.Setenv("TERRAGRUNT_LOG_LEVEL", "unparsable")

	cleanupTerraformFolder(t, TEST_FIXTURE_INPUTS)
	tmpEnvPath := copyEnvironment(t, ".")
	rootPath := util.JoinPath(tmpEnvPath, TEST_FIXTURE_INPUTS)

	// Ideally, we would check stderr to introspect the error message, but the global fallback logger only logs to real
	// stderr and we can't capture the output, so in this case we only make sure that the command runs successfully to
	// completion.
	runTerragrunt(t, fmt.Sprintf("terragrunt validate --terragrunt-non-interactive --terragrunt-working-dir %s", rootPath))
}

// NOTE: the following test requires precise timing for determining parallelism. As such, it can not be run in parallel
// with all the other tests as the system load could impact the duration in which the parallel terragrunt goroutines
// run.

func testTerragruntParallelism(t *testing.T, parallelism int, numberOfModules int, timeToDeployEachModule time.Duration, expectedTimings []int) {
	output, testStart, err := testRemoteFixtureParallelism(t, parallelism, numberOfModules, timeToDeployEachModule)
	require.NoError(t, err)

	// parse output and sort the times, the regex captures a string in the format time.RFC3339 emitted by terraform's timestamp function
	r, err := regexp.Compile(`out = "([-:\w]+)"`)
	require.NoError(t, err)

	matches := r.FindAllStringSubmatch(output, -1)
	assert.Equal(t, numberOfModules, len(matches))
	var times []int
	for _, v := range matches {
		// timestamp() is parsed
		parsed, err := time.Parse(time.RFC3339, v[1])
		require.NoError(t, err)
		times = append(times, int(parsed.Unix())-testStart)
	}
	sort.Slice(times, func(i, j int) bool {
		return times[i] < times[j]
	})

	// the reported times are skewed (running terragrunt/terraform apply adds a little bit of overhead)
	// we apply a simple scaling algorithm on the times based on the last expected time and the last actual time
	k := float64(times[len(times)-1]) / float64(expectedTimings[len(expectedTimings)-1])

	scaledTimes := make([]float64, len(times))
	for i := 0; i < len(times); i++ {
		scaledTimes[i] = float64(times[i]) / k
	}

	t.Logf("Parallelism test numberOfModules=%d p=%d expectedTimes=%v times=%v scaledTimes=%v scaleFactor=%f", numberOfModules, parallelism, expectedTimings, times, scaledTimes, k)

	maxDiffInSeconds := 5.0
	isEqual := func(x, y float64) bool {
		return math.Abs(x-y) <= maxDiffInSeconds
	}
	for i := 0; i < len(times); i++ {
		// it's impossible to know when will the first test finish however once a test finishes
		// we know that all the other times are relative to the first one
		assert.True(t, isEqual(scaledTimes[i], float64(expectedTimings[i])))
	}
}

func TestTerragruntParallelism(t *testing.T) {
	testCases := []struct {
		parallelism            int
		numberOfModules        int
		timeToDeployEachModule time.Duration
		expectedTimings        []int
	}{
		{1, 10, 5 * time.Second, []int{5, 10, 15, 20, 25, 30, 35, 40, 45, 50}},
		{3, 10, 5 * time.Second, []int{5, 5, 5, 10, 10, 10, 15, 15, 15, 20}},
		{5, 10, 5 * time.Second, []int{5, 5, 5, 5, 5, 5, 5, 5, 5, 5}},
	}
	for _, tc := range testCases {
		tc := tc // shadow and force execution with this case
		t.Run(fmt.Sprintf("parallelism=%d numberOfModules=%d timeToDeployEachModule=%v expectedTimings=%v", tc.parallelism, tc.numberOfModules, tc.timeToDeployEachModule, tc.expectedTimings), func(t *testing.T) {
			testTerragruntParallelism(t, tc.parallelism, tc.numberOfModules, tc.timeToDeployEachModule, tc.expectedTimings)
		})
	}
}

func TestTerragruntWorksWithImpersonateGCSBackend(t *testing.T) {
	impersonatorKey := os.Getenv("GCLOUD_SERVICE_KEY_IMPERSONATOR")
	if impersonatorKey == "" {
		t.Fatalf("Required environment variable `%s` - not found", "GCLOUD_SERVICE_KEY_IMPERSONATOR")
	}
	tmpImpersonatorCreds := createTmpTerragruntConfigContent(t, impersonatorKey, "impersonator-key.json")
	defer removeFile(t, tmpImpersonatorCreds)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", tmpImpersonatorCreds)

	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	gcsBucketName := fmt.Sprintf("terragrunt-test-bucket-%s", strings.ToLower(uniqueId()))

	// run with impersonation
	tmpTerragruntImpersonateGCSConfigPath := createTmpTerragruntGCSConfig(t, TEST_FIXTURE_GCS_IMPERSONATE_PATH, project, TERRAFORM_REMOTE_STATE_GCP_REGION, gcsBucketName, config.DefaultTerragruntConfigPath)
	runTerragrunt(t, fmt.Sprintf("terragrunt apply -auto-approve --terragrunt-non-interactive --terragrunt-config %s --terragrunt-working-dir %s", tmpTerragruntImpersonateGCSConfigPath, TEST_FIXTURE_GCS_IMPERSONATE_PATH))

	var expectedGCSLabels = map[string]string{
		"owner": "terragrunt_test",
		"name":  "terraform_state_storage"}
	validateGCSBucketExistsAndIsLabeled(t, TERRAFORM_REMOTE_STATE_GCP_REGION, gcsBucketName, expectedGCSLabels)

	email := os.Getenv("GOOGLE_IDENTITY_EMAIL")
	attrs := gcsObjectAttrs(t, gcsBucketName, "terraform.tfstate/default.tfstate")
	ownerEmail := false
	for _, a := range attrs.ACL {
		if (a.Role == "OWNER") && (a.Email == email) {
			ownerEmail = true
			break
		}
	}
	assert.True(t, ownerEmail, "Identity email should match the impersonated account")
}

func TestTerragruntProduceTelemetryTraces(t *testing.T) {
	t.Setenv("TERRAGRUNT_TELEMETRY_TRACE_EXPORTER", "console")

	cleanupTerraformFolder(t, TEST_FIXTURE_HOOKS_BEFORE_AND_AFTER_PATH)
	tmpEnvPath := copyEnvironment(t, TEST_FIXTURE_HOOKS_BEFORE_AND_AFTER_PATH)
	rootPath := util.JoinPath(tmpEnvPath, TEST_FIXTURE_HOOKS_BEFORE_AND_AFTER_PATH)

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	err := runTerragruntCommand(t, fmt.Sprintf("terragrunt apply -auto-approve --terragrunt-non-interactive --terragrunt-working-dir %s", rootPath), &stdout, &stderr)
	assert.NoError(t, err)

	output := stdout.String()

	// check that output have Telemetry json output
	assert.Contains(t, output, "\"SpanContext\":")
	assert.Contains(t, output, "\"TraceID\":")
	assert.Contains(t, output, "\"Name\":\"hook_after_hook_1\"")
	assert.Contains(t, output, "\"Name\":\"hook_after_hook_2\"")
}

func TestTerragruntProduceTelemetryMetrics(t *testing.T) {
	t.Setenv("TERRAGRUNT_TELEMETRY_METRIC_EXPORTER", "console")

	cleanupTerraformFolder(t, TEST_FIXTURE_HOOKS_BEFORE_AND_AFTER_PATH)
	tmpEnvPath := copyEnvironment(t, TEST_FIXTURE_HOOKS_BEFORE_AND_AFTER_PATH)
	rootPath := util.JoinPath(tmpEnvPath, TEST_FIXTURE_HOOKS_BEFORE_AND_AFTER_PATH)

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	err := runTerragruntCommand(t, fmt.Sprintf("terragrunt apply -no-color -auto-approve --terragrunt-non-interactive --terragrunt-working-dir %s", rootPath), &stdout, &stderr)
	assert.NoError(t, err)

	// sleep for a bit to allow the metrics to be flushed
	time.Sleep(1 * time.Second)

	output := stdout.String()

	// check that output have Telemetry json output
	assert.Contains(t, output, "{\"Name\":\"hook_after_hook_2_duration\"")
	assert.Contains(t, output, "{\"Name\":\"run_")
	assert.Contains(t, output, ",\"IsMonotonic\":true}}")
}

func TestTerragruntOutputJson(t *testing.T) {
	// no parallel test execution since JSON output is global
	defer func() {
		util.DisableJsonFormat()
	}()

	tmpEnvPath := copyEnvironment(t, TEST_FIXTURE_NOT_EXISTING_SOURCE)
	cleanupTerraformFolder(t, tmpEnvPath)
	testPath := util.JoinPath(tmpEnvPath, TEST_FIXTURE_NOT_EXISTING_SOURCE)

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	err := runTerragruntCommand(t, fmt.Sprintf("terragrunt apply --terragrunt-json-log --terragrunt-non-interactive --terragrunt-working-dir %s", testPath), &stdout, &stderr)
	assert.Error(t, err)

	var output map[string]interface{}

	err = json.Unmarshal(stderr.Bytes(), &output)
	assert.NoError(t, err)

	assert.Contains(t, output["msg"], "Downloading Terraform configurations from git::https://github.com/gruntwork-io/terragrunt.git?ref=v0.9.9")
}

func TestTerragruntTerraformOutputJson(t *testing.T) {
	// no parallel test execution since JSON output is global
	defer func() {
		util.DisableJsonFormat()
	}()

	tmpEnvPath := copyEnvironment(t, TEST_FIXTURE_INIT_ERROR)
	cleanupTerraformFolder(t, tmpEnvPath)
	testPath := util.JoinPath(tmpEnvPath, TEST_FIXTURE_INIT_ERROR)

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	err := runTerragruntCommand(t, fmt.Sprintf("terragrunt apply --no-color --terragrunt-json-log --terragrunt-tf-logs-to-json --terragrunt-non-interactive --terragrunt-working-dir %s", testPath), &stdout, &stderr)
	assert.Error(t, err)

	assert.Contains(t, stderr.String(), "\"level\":\"info\",\"msg\":\"Initializing the backend...")

	// check if output can be extracted in json
	jsonStrings := strings.Split(stderr.String(), "\n")
	for _, jsonString := range jsonStrings {
		if len(jsonString) == 0 {
			continue
		}
		var output map[string]interface{}
		err = json.Unmarshal([]byte(jsonString), &output)
		assert.NoErrorf(t, err, "Failed to parse json %s", jsonString)
		assert.NotNil(t, output["level"])
		assert.NotNil(t, output["level"])
		assert.NotNil(t, output["time"])
	}
}
