include ./common.mk

test: test-cdk test-awscc test-aws test-cfncompat ## Run all four suites (TestAwsCdk, TestTerraformAwscc, TestTerraformAws, TestTerraformCfncompat)
.PHONY: test

test-cdk: ## Run TestAwsCdk only (expected GREEN)
	go test -v -count 1 -timeout 60m -run '^TestAwsCdk$$' ./...
.PHONY: test-cdk

test-awscc: ## Run TestTerraformAwscc only (expected RED starting at deploy B)
	go test -v -count 1 -timeout 60m -run '^TestTerraformAwscc$$' ./...
.PHONY: test-awscc

test-aws: ## Run TestTerraformAws only (aws-vs-awscc comparison, see harness_test.go)
	go test -v -count 1 -timeout 60m -run '^TestTerraformAws$$' ./...
.PHONY: test-aws

test-cfncompat: ## Run TestTerraformCfncompat only (expected GREEN, see harness_test.go)
	go test -v -count 1 -timeout 60m -run '^TestTerraformCfncompat$$' ./...
.PHONY: test-cfncompat
