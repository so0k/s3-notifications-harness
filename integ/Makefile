include ./common.mk

test: test-cdk test-tf ## Run both suites (TestAwsCdk, then TestTerraform)
.PHONY: test

test-cdk: ## Run TestAwsCdk only (expected GREEN)
	go test -v -count 1 -timeout 60m -run '^TestAwsCdk$$' ./...
.PHONY: test-cdk

test-tf: ## Run TestTerraform only (expected RED starting at deploy B)
	go test -v -count 1 -timeout 60m -run '^TestTerraform$$' ./...
.PHONY: test-tf
