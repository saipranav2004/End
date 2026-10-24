// Package workloadtypes decides which kinds of workload NHI Genome watches.
//
// Every discovered identity has a type (lambda_function, ecs_task, ...) that
// belongs to a compact family (LAMBDA, ECS, ...), the source_type the behaviour
// pipeline stores. NHI_GENOME_WORKLOAD_TYPES lists the families to show, for
// example "ECS,EC2,GLUE,LAMBDA,STEPFUNCTION"; empty shows every family.
package workloadtypes

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// families maps each monitored identity type to its source_type family. It
// follows the contract's NHI type table; types fed by an external audit log
// (GitHub Actions, GitLab CI, Terraform) or attributed only conditionally (IoT
// things, Greengrass) are not monitored.
var families = map[string]string{
	"ec2_instance": "EC2", "lambda_function": "LAMBDA", "ecs_task": "ECS",
	"app_runner_service": "APP_RUNNER", "elastic_beanstalk_environment": "ELASTIC_BEAN",
	"ssm_managed_node": "SSM", "ssm_automation": "SSM",
	"glue_job": "GLUE", "glue_crawler": "GLUE", "emr_cluster": "EMR", "batch_job": "BATCH",
	"dms_replication_instance": "DMS", "kinesis_firehose": "KINESIS", "kinesis_analytics_app": "KINESIS",
	"datasync_task":      "DATASYNC",
	"sagemaker_notebook": "SAGEMAKER", "sagemaker_training_job": "SAGEMAKER",
	"sagemaker_processing_job": "SAGEMAKER", "sagemaker_endpoint": "SAGEMAKER",
	"bedrock_agent": "BEDROCK", "bedrock_knowledge_base": "BEDROCK",
	"codebuild_project": "CODEBUILD", "codepipeline": "CODEPIPELINE", "codedeploy_service_role": "CODEDEPLOY",
	"cloudformation_stack_role": "CLOUDFORMATION", "mwaa_environment": "MWAA",
	"step_function": "STEP_FUNCTIONS", "eventbridge_scheduler": "EVENTBRIDGE", "synthetics_canary": "SYNTHETICS",
	"iot_role_alias": "IOT", "transfer_server": "TRANSFER",
	"iam_roles_anywhere_session": "IAM", "saas_vendor_role": "IAM", "aws_service_linked_role": "AWS_SLR",
}

// aliases maps other spellings seen in scanner output to the identity type.
var aliases = map[string]string{
	"aws::ec2::instance": "ec2_instance", "aws::lambda::function": "lambda_function",
	"aws::ecs::task": "ecs_task", "aws::ecs::service": "ecs_task", "ecs_service": "ecs_task",
	"aws::stepfunctions::statemachine": "step_function", "step_functions": "step_function",
	"aws::glue::job": "glue_job", "aws::glue::crawler": "glue_crawler",
	"aws::codebuild::project": "codebuild_project", "aws::codepipeline::pipeline": "codepipeline",
	"aws::sagemaker::notebookinstance": "sagemaker_notebook", "aws::ssm::managedinstance": "ssm_managed_node",
	"aws::apprunner::service": "app_runner_service",
}

var (
	mu      sync.RWMutex
	enabled map[string]struct{} // nil: every family
)

// norm makes "STEP_FUNCTIONS", "StepFunction" and "stepfunctions" the same key.
func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return strings.TrimSuffix(b.String(), "S")
}

// Families is every family NHI Genome can watch, sorted.
func Families() []string {
	set := map[string]struct{}{}
	for _, f := range families {
		set[f] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Configure sets the workload types to show from a comma-separated list.
// Empty means all of them. An unknown name is an error, so a typo cannot
// silently hide everything.
func Configure(raw string) error {
	if strings.TrimSpace(raw) == "" {
		mu.Lock()
		enabled = nil
		mu.Unlock()
		return nil
	}
	byNorm := map[string]string{}
	for _, f := range Families() {
		byNorm[norm(f)] = f
	}
	next := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		f, ok := byNorm[norm(part)]
		if !ok {
			return fmt.Errorf("config: NHI_GENOME_WORKLOAD_TYPES has an unknown workload type %q; known: %s", strings.TrimSpace(part), strings.Join(Families(), ", "))
		}
		next[f] = struct{}{}
	}
	mu.Lock()
	if len(next) == 0 {
		enabled = nil
	} else {
		enabled = next
	}
	mu.Unlock()
	return nil
}

// Allows reports whether NHI Genome shows this family.
func Allows(family string) bool {
	mu.RLock()
	defer mu.RUnlock()
	if enabled == nil {
		return true
	}
	_, ok := enabled[family]
	return ok
}

// Enabled is the families shown now and whether that is all of them.
func Enabled() (list []string, all bool) {
	mu.RLock()
	defer mu.RUnlock()
	if enabled == nil {
		return Families(), true
	}
	for f := range enabled {
		list = append(list, f)
	}
	sort.Strings(list)
	return list, false
}

// Resolve maps a discovery identity type to its monitoring type and family. ok
// is false for a type that is not monitored or whose family is switched off.
func Resolve(identityType string) (subtype, family string, ok bool) {
	key := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(identityType)))
	if alias, found := aliases[key]; found {
		key = alias
	}
	family, known := families[key]
	if !known || !Allows(family) {
		return key, family, false
	}
	return key, family, true
}
