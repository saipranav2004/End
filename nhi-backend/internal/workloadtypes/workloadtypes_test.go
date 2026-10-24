package workloadtypes

import "testing"

func TestConfigureAndResolve(t *testing.T) {
	t.Cleanup(func() { _ = Configure("") })
	if _, _, ok := Resolve("glue_job"); !ok {
		t.Fatal("empty setting shows every family")
	}
	if err := Configure("ECS, EC2,GLUE,LAMBDA,STEPFUNCTION"); err != nil {
		t.Fatal(err)
	}
	for typ, want := range map[string]bool{
		"ecs_task": true, "ec2_instance": true, "glue_crawler": true, "lambda_function": true,
		"step_function": true, "sagemaker_endpoint": false, "codebuild_project": false, "not_a_type": false,
	} {
		if _, _, ok := Resolve(typ); ok != want {
			t.Errorf("%s: got %v want %v", typ, ok, want)
		}
	}
	if list, all := Enabled(); all || len(list) != 5 {
		t.Errorf("enabled %v all=%v", list, all)
	}
	if err := Configure("LAMBDA,STEP_FUNCTIONS"); err != nil || !Allows("STEP_FUNCTIONS") {
		t.Errorf("STEP_FUNCTIONS spelling: %v", err)
	}
	if err := Configure("LAMDBA"); err == nil {
		t.Error("a typo must be rejected, not hide everything")
	}
}
