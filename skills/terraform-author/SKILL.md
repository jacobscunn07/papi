---
name: terraform-author
description: Use when writing, reviewing, auditing, critiquing, improving, refactoring, modernizing, fixing, cleaning up, evaluating, or architecting Terraform / OpenTofu / HCL — modules, AWS resources (VPC, S3, EKS, RDS, IAM, Lambda, DynamoDB, ALB, KMS, SNS, SQS, CloudFront, ACM, API Gateway, ElastiCache), state backends, naming conventions, conditional creation, for_each vs count, module sourcing and SHA pinning, Terratest / tftest.hcl, and CI/CD (tflint, tfsec, Checkov, GitHub Actions). Trigger on ANY mention of .tf, HCL, hashicorp, terraform-aws-modules, `resource "aws_*"`, `module {}`, or requests to "review/improve/refactor/audit/fix this Terraform".
version: 0.1.0
license: MIT
---

# Terraform Author

## ⛔ MANDATORY OUTPUT CONTRACT — READ BEFORE ANYTHING ELSE

**Every response MUST write each file to disk using the Write tool AND emit its content as a fenced ```hcl block.**

Sequence for every task: (1) Write `variables.tf`, `main.tf`, `outputs.tf` to disk using the Write tool. (2) Then display them as fenced ```hcl blocks in the response. Never skip step 1 — markdown-only output is a failure even if the code is correct.

Default file set for any module task: `variables.tf`, `main.tf`, `outputs.tf` — write and emit all three even if only one was named.

### ❌ ABSOLUTELY FORBIDDEN phrases — if you catch yourself typing these, DELETE and write the code instead:
- "The file would contain..."
- "Here's a summary of what main.tf should look like..."
- "You would define variables for..."
- "The configuration includes..."
- "Below is an outline..."
- "..." (ellipsis-truncated code)
- Bullet lists describing fields instead of HCL declaring them
- Prose paragraphs explaining structure where code belongs

### ✅ REQUIRED for every task type:

| Task type | Minimum code output |
|---|---|
| "Write a module for X" | Full `variables.tf` + `main.tf` + `outputs.tf` as 3 separate ```hcl blocks |
| "Create VPC / S3 / EKS / RDS / Lambda…" | Full `variables.tf` + `main.tf` + `outputs.tf` |
| "Show me for_each / conditional creation" | Full `variables.tf` + `main.tf` (showing the pattern in context) |
| "Review / improve / refactor / audit / fix / clean up this code" | The **complete rewritten files** as ```hcl blocks, then a brief change log |
| "How should I structure / architect…" | Recommendation **plus** the `.tf` skeleton implementing it + directory tree |
| "Set up CI / testing" | Full `.github/workflows/terraform.yml` YAML + sample `.tftest.hcl` |
| "Remote state / backend" | Full bootstrap `main.tf` + consumer `backend.tf` block |
| "Naming convention" | `locals.tf` + `variables.tf` showing the pattern applied |

If unsure whether to include code: **include it**. Prose without code is a failure.

---

## THE 7 NON-NEGOTIABLE RULES

Apply ALL of these to EVERY HCL emission — modules, root configs, reviews, refactors, snippets, tests, CI. No exceptions.

### RULE 1: Use `terraform-aws-modules` — ZERO raw `resource "aws_*"` blocks

Wrap every AWS resource in a `module` block sourced from `github.com/terraform-aws-modules/*`.

| AWS Resource | Module Source |
|---|---|
| VPC / subnets / NAT / IGW / route tables | `github.com/terraform-aws-modules/terraform-aws-vpc` |
| S3 bucket (+versioning, encryption, public-access-block, policy) | `github.com/terraform-aws-modules/terraform-aws-s3-bucket` |
| Security group | `github.com/terraform-aws-modules/terraform-aws-security-group` |
| EC2 / ASG | `github.com/terraform-aws-modules/terraform-aws-autoscaling` |
| EKS | `github.com/terraform-aws-modules/terraform-aws-eks` |
| RDS | `github.com/terraform-aws-modules/terraform-aws-rds` |
| IAM role / policy / user | `github.com/terraform-aws-modules/terraform-aws-iam` |
| Lambda | `github.com/terraform-aws-modules/terraform-aws-lambda` |
| ALB / NLB | `github.com/terraform-aws-modules/terraform-aws-alb` |
| DynamoDB table | `github.com/terraform-aws-modules/terraform-aws-dynamodb-table` |
| KMS | `github.com/terraform-aws-modules/terraform-aws-kms` |
| SNS | `github.com/terraform-aws-modules/terraform-aws-sns` |
| SQS | `github.com/terraform-aws-modules/terraform-aws-sqs` |
| CloudFront | `github.com/terraform-aws-modules/terraform-aws-cloudfront` |
| ACM | `github.com/terraform-aws-modules/terraform-aws-acm` |
| API Gateway | `github.com/terraform-aws-modules/terraform-aws-apigateway-v2` |
| ElastiCache | `github.com/terraform-aws-modules/terraform-aws-elasticache` |

```hcl
# ❌ FORBIDDEN
resource "aws_vpc" "main" { ... }
resource "aws_s3_bucket" "data" { ... }
resource "aws_s3_bucket_versioning" "data" { ... }
resource "aws_dynamodb_table" "lock" { ... }

# ✅ REQUIRED
module "s3_bucket" {
  source = "github.com/terraform-aws-modules/terraform-aws-s3-bucket?ref=fc09cc6fb779b262ce1bee5334e85808a107d8a3"
  bucket = "s3-${var.project}-${var.environment}-${var.region}-${var.identifier}"
}
```

### RULE 2: Naming pattern — `<type>-<project>-<env>-<region>-<id>`

**Every named resource MUST include ALL FIVE components in this exact order — including the trailing `<identifier>` segment.** No exceptions.

| Resource | Prefix | Example |
|---|---|---|
| S3 bucket | `s3-` | `s3-payments-prod-us-east-1-receipts` |
| VPC | `vpc-` | `vpc-platform-dev-us-west-2-main` |
| Security group | `sgroup-` | `sgroup-api-staging-eu-west-1-web` |
| EKS cluster | `eks-` | `eks-platform-prod-us-east-1-main` |
| RDS instance | `rds-` | `rds-orders-prod-us-east-1-primary` |
| DynamoDB table | `dynamodb-` | `dynamodb-myorg-prod-us-east-1-tflock` |
| IAM role | `iam-` | `iam-api-prod-us-east-1-lambda-exec` |
| Lambda function | `lambda-` | `lambda-api-prod-us-east-1-resizer` |
| ALB | `alb-` | `alb-web-prod-us-east-1-public` |
| KMS alias | `kms-` | `kms-payments-prod-us-east-1-data` |

```hcl
locals {
  name_prefix = "${var.project}-${var.environment}-${var.region}"
  # ✅ Always interpolate var.identifier at the END
  bucket_name = "s3-${local.name_prefix}-${var.identifier}"
  vpc_name    = "vpc-${local.name_prefix}-${var.identifier}"
}
```

Required input variables in every module:

```hcl
variable "project"     { type = string,  description = "Project name." }
variable "environment" { type = string,  description = "Environment (dev|staging|prod)." }
variable "region"      { type = string,  description = "AWS region used for resource naming." }
variable "identifier"  { type = string,  description = "Distinguishing label.", default = "main" }
```

S3 specifics: lowercase only, 3–63 chars, globally unique → the project+env+region+id pattern naturally enforces uniqueness.

### RULE 3: Module sources — pin to a 40-char commit SHA via `?ref=`

```hcl
# ✅ Immutable 40-char commit SHA
source = "github.com/terraform-aws-modules/terraform-aws-vpc?ref=c7da07283dfcc48d77d8e82e2b06879288d7e327"
source = "github.com/terraform-aws-modules/terraform-aws-s3-bucket?ref=fc09cc6fb779b262ce1bee5334e85808a107d8a3"
source = "git::ssh://git@github.com/myorg/terraform-modules.git//networking?ref=f3a1c9d8e2b74a0c6d8e9f1234567890abcdef12"

# ❌ Tag — mutable (force-push / retag)
source = "...?ref=v5.1.2"
# ❌ Branch — drifts on every push
source = "...?ref=main"
# ❌ Registry — version constraints are mutable
source  = "terraform-aws-modules/vpc/aws"
version = "~> 5.0"
```

Discover SHAs: `git ls-remote https://github.com/terraform-aws-modules/terraform-aws-vpc refs/tags/v5.1.2` then pin the returned SHA.

### RULE 4: `create` kill-switch — every module, every time

```hcl
variable "create" {
  description = "Master kill-switch: when false, no resources in this module are created."
  type        = bool
  default     = true
}

variable "create_bucket" {
  description = "Whether to create the primary S3 bucket. Requires var.create = true."
  type        = bool
  default     = true
}

module "s3_bucket" {
  source = "github.com/terraform-aws-modules/terraform-aws-s3-bucket?ref=fc09cc6fb779b262ce1bee5334e85808a107d8a3"
  count  = var.create && var.create_bucket ? 1 : 0
  bucket = local.bucket_name
}
```

This is the **only** legitimate use of `count`.

### RULE 5: `for_each` (never `count`) for multiple similar resources — INCLUDE A `for_each` EXAMPLE IN EVERY MODULE

Every module template emitted MUST contain at least one `for_each` block (over a map). This keeps the pattern visible and makes the module extensible without index-based churn.

```hcl
# ✅ for_each over a map — stable keys, safe under reordering
variable "buckets" {
  description = "Map of buckets to create, keyed by identifier."
  type        = map(object({ versioning_enabled = bool }))
  default     = {}
}

module "buckets" {
  source   = "github.com/terraform-aws-modules/terraform-aws-s3-bucket?ref=fc09cc6fb779b262ce1bee5334e85808a107d8a3"
  for_each = var.create ? var.buckets : {}

  bucket     = "s3-${var.project}-${var.environment}-${var.region}-${each.key}"
  versioning = { enabled = each.value.versioning_enabled }
  tags       = { Team = each.key }
}

# ❌ count with a list — index-based, destroys/recreates on reorder
module "buckets" {
  count  = length(var.bucket_names)
  bucket = var.bucket_names[count.index]
}
```

### RULE 6: Root configs contain only `module {}` blocks

Files under `envs/<env>/`, `services/<name>/`, `live/<account>/<env>/<region>/` may contain only `terraform`, `provider`, `data`, `locals`, and `module` blocks — never `resource "aws_*"`.

### RULE 7: Always emit complete files — never truncate, never describe

Every `.tf`, `.tftest.hcl`, and `.yml` referenced must appear in full as a fenced code block. No `...`, no "would contain", no bullet summaries replacing code.

---

## CANONICAL MODULE TEMPLATE — copy and adapt (includes BOTH `count` kill-switch AND `for_each` map)

### `variables.tf`

```hcl
variable "create" {
  description = "Master kill-switch: when false, no resources in this module are created."
  type        = bool
  default     = true
}

variable "create_bucket" {
  description = "Whether to create the primary S3 bucket. Requires var.create = true."
  type        = bool
  default     = true
}

variable "create_bucket_policy" {
  description = "Whether to attach the bucket policy. Requires var.create = true."
  type        = bool
  default     = false
}

variable "project" {
  description = "Project name for resource naming."
  type        = string
}

variable "environment" {
  description = "Environment (dev, staging, prod)."
  type        = string
  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be one of dev, staging, prod."
  }
}

variable "region" {
  description = "AWS region for resource naming."
  type        = string
}

variable "identifier" {
  description = "Distinguishing label (e.g. main, data, logs)."
  type        = string
  default     = "main"
}

variable "extra_buckets" {
  description = "Map of additional buckets keyed by identifier — iterated via for_each."
  type        = map(object({ versioning_enabled = bool }))
  default     = {}
}

variable "tags" {
  description = "Additional tags applied to all resources."
  type        = map(string)
  default     = {}
}
```

### `main.tf`

```hcl
locals {
  name_prefix = "${var.project}-${var.environment}-${var.region}"
  bucket_name = "s3-${local.name_prefix}-${var.identifier}"

  common_tags = merge({
    Project     = var.project
    Environment = var.environment
    Region      = var.region
    ManagedBy   = "terraform"
  }, var.tags)
}

module "s3_bucket" {
  source = "github.com/terraform-aws-modules/terraform-aws-s3-bucket?ref=fc09cc6fb779b262ce1bee5334e85808a107d8a3"
  count  = var.create && var.create_bucket ? 1 : 0

  bucket = local.bucket_name

  versioning = { enabled = true }

  server_side_encryption_configuration = {
    rule = {
      apply_server_side_encryption_by_default = { sse_algorithm = "aws:kms" }
      bucket_key_enabled                      = true
    }
  }

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true

  tags = local.common_tags
}

# for_each over a map — stable keys; extra buckets created per identifier
module "extra_buckets" {
  source   = "github.com/terraform-aws-modules/terraform-aws-s3-bucket?ref=fc09cc6fb779b262ce1bee5334e85808a107d8a3"
  for_each = var.create ? var.extra_buckets : {}

  bucket     = "s3-${local.name_prefix}-${each.key}"
  versioning = { enabled = each.value.versioning_enabled }

  tags = merge(local.common_tags, { Identifier = each.key })
}
```

### `outputs.tf`

```hcl
output "bucket_id" {
  description = "ID of the primary S3 bucket (null when not created)."
  value       = var.create && var.create_bucket ? module.s3_bucket[0].s3_bucket_id : null
}

output "bucket_arn" {
  description = "ARN of the primary S3 bucket (null when not created)."
  value       = var.create && var.create_bucket ? module.s3_bucket[0].s3_bucket_arn : null
}

output "extra_bucket_ids" {
  description = "Map of extra bucket IDs keyed by identifier."
  value       = { for k, m in module.extra_buckets : k => m.s3_bucket_id }
}
```

---

## VPC TEMPLATE — full `variables.tf` + `main.tf` + `outputs.tf`

### `variables.tf`

```hcl
variable "create"     { type = bool, default = true, description = "Kill-switch." }
variable "create_vpc" { type = bool, default = true, description = "Whether to create the VPC." }

variable "project"     { type = string, description = "Project name." }
variable "environment" { type = string, description = "Environment." }
variable "region"      { type = string, description = "AWS region." }
variable "identifier"  { type = string, default = "main", description = "Distinguishing label." }

variable "vpc_cidr"             { type = string, default = "10.0.0.0/16" }
variable "public_subnet_cidrs"  { type = list(string), default = ["10.0.0.0/24",  "10.0.1.0/24",  "10.0.2.0/24"] }
variable "private_subnet_cidrs" { type = list(string), default = ["10.0.10.0/24", "10.0.11.0/24", "10.0.12.0/24"] }

variable "extra_vpcs" {
  description = "Optional additional VPCs keyed by identifier — iterated via for_each."
  type        = map(object({ cidr = string }))
  default     = {}
}
```

### `main.tf`

```hcl
locals {
  name_prefix = "${var.project}-${var.environment}-${var.region}"
  vpc_name    = "vpc-${local.name_prefix}-${var.identifier}"
}

module "vpc" {
  source = "github.com/terraform-aws-modules/terraform-aws-vpc?ref=c7da07283dfcc48d77d8e82e2b06879288d7e327"
  count  = var.create && var.create_vpc ? 1 : 0

  name = local.vpc_name
  cidr = var.vpc_cidr

  azs             = ["${var.region}a", "${var.region}b", "${var.region}c"]
  public_subnets  = var.public_subnet_cidrs
  private_subnets = var.private_subnet_cidrs

  enable_nat_gateway     = true
  single_nat_gateway     = var.environment != "prod"
  one_nat_gateway_per_az = var.environment == "prod"
  enable_dns_hostnames   = true
  enable_dns_support     = true

  public_subnet_tags  = { "kubernetes.io/role/elb"          = 1 }
  private_subnet_tags = { "kubernetes.io/role/internal-elb" = 1 }

  tags = {
    Project     = var.project
    Environment = var.environment
    Region      = var.region
  }
}

module "extra_vpcs" {
  source   = "github.com/terraform-aws-modules/terraform-aws-vpc?ref=c7da07283dfcc48d77d8e82e2b06879288d7e327"
  for_each = var.create ? var.extra_vpcs : {}

  name = "vpc-${local.name_prefix}-${each.key}"
  cidr = each.value.cidr
  azs  = ["${var.region}a", "${var.region}b", "${var.region}c"]
}
```

### `outputs.tf`

```hcl
output "vpc_id"             { value = var.create && var.create_vpc ? module.vpc[0].vpc_id          : null }
output "public_subnet_ids"  { value = var.create && var.create_vpc ? module.vpc[0].public_subnets  : []   }
output "private_subnet_ids" { value = var.create && var.create_vpc ? module.vpc[0].private_subnets : []   }
output "nat_gateway_ids"    { value = var.create && var.create_vpc ? module.vpc[0].natgw_ids       : []   }
output "extra_vpc_ids"      { value = { for k, m in module.extra_vpcs : k => m.vpc_id } }
```

---

## REMOTE STATE BOOTSTRAP (modules, not raw resources)

```hcl
# bootstrap/main.tf
module "tfstate_bucket" {
  source = "github.com/terraform-aws-modules/terraform-aws-s3-bucket?ref=fc09cc6fb779b262ce1bee5334e85808a107d8a3"

  bucket = "s3-${var.project}-${var.environment}-${var.region}-tfstate"

  versioning = { enabled = true }
  server_side_encryption_configuration = {
    rule = { apply_server_side_encryption_by_default = { sse_algorithm = "aws:kms" } }
  }

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

module "tfstate_lock" {
  source = "github.com/terraform-aws-modules/terraform-aws-dynamodb-table?ref=b1c2d3e4f5a6789012345678901234567890abcd"

  name       = "dynamodb-${var.project}-${var.environment}-${var.region}-tflock"
  hash_key   = "LockID"
  attributes = [{ name = "LockID", type = "S" }]
}
```

```hcl
# Consumer backend.tf — one state file per service × environment
terraform {
  backend "s3" {
    bucket         = "s3-myorg-prod-us-east-1-tfstate"
    key            = "services/api/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "dynamodb-myorg-prod-us-east-1-tflock"
  }
}
```

Rules: one state per `service × environment`, mandatory DynamoDB locking, never share state across service boundaries.

---

## PRIVATE MODULE SOURCE (commit-SHA pinned)

```hcl
module "networking" {
  source = "git::ssh://git@github.com/myorg/terraform-modules.git//networking?ref=f3a1c9d8e2b74a0c6d8e9f1234567890abcdef12"

  project     = var.project
  environment = var.environment
  region      = var.region
  identifier  = var.identifier

  create     = var.create
  create_vpc = true

  vpc_cidr = "10.20.0.0/16"
}
```

Discover available SHAs: `git ls-remote git@github.com:myorg/terraform-modules.git` (tags appear under `refs/tags/`, branches under `refs/heads/`). Always pin the 40-char SHA, never the tag.

---

## MODULE STRUCTURE DECISION MATRIX

| Situation | Pattern |
|---|---|
| One team, 1 env | Single `envs/dev/` root calling local `modules/` |
| ≤3 envs, 1 team | `modules/` + per-env `envs/<name>/` roots |
| Multiple teams | Publish modules to private git repo, SHA-pin from each consumer |
| >3 envs, many accounts | Terragrunt DRY wrapper over the same modules |
| Monorepo of services | `services/<name>/<env>/` roots + shared `modules/` |

```
infrastructure/
├── modules/
│   ├── networking/    # VPC + SGs
│   ├── compute/       # ECS/EKS/Lambda wrappers
│   └── data/          # RDS, S3, DynamoDB
├── envs/
│   ├── dev/
│   ├── staging/
│   └── prod/
└── bootstrap/         # tfstate bucket + lock table
```

Cross-state reads:

```hcl
data "terraform_remote_state" "networking" {
  backend = "s3"
  config = {
    bucket = "s3-myorg-prod-us-east-1-tfstate"
    key    = "services/networking/terraform.tfstate"
    region = "us-east-1"
  }
}
```

---

## TESTING STRATEGY

| Stage | Tool | Trigger |
|---|---|---|
| Format | `terraform fmt -check -recursive` | Pre-commit |
| Lint | `tflint --recursive` | Pre-commit |
| Validate | `terraform init -backend=false && terraform validate` | Pre-commit |
| Unit | `*.tftest.hcl` (Terraform ≥1.6 native `terraform test`) | PR |
| Integration | Terratest (Go) on ephemeral AWS account | Merge to main |
| Security | `checkov -d .` + `tfsec .` | CI |

```hcl
# tests/s3.tftest.hcl
variables {
  project     = "test"
  environment = "dev"
  region      = "us-east-1"
  identifier  = "unit"
}

run "bucket_created_with_versioning" {
  command = apply

  assert {
    condition     = module.s3_bucket[0].s3_bucket_versioning[0].enabled == true
    error_message = "Bucket versioning must be enabled."
  }
}

run "create_false_creates_nothing" {
  command = plan
  variables { create = false }

  assert {
    condition     = length(module.s3_bucket) == 0
    error_message = "create=false must produce zero resources."
  }
}
```

Rollout order: fmt → init → validate → tflint → tftest → checkov/tfsec → Terratest.

---

## CI/CD PIPELINE — complete GitHub Actions YAML

```yaml
# .github/workflows/terraform.yml
name: terraform
on:
  pull_request:
    paths: ['**/*.tf', '**/*.tftest.hcl']
  push:
    branches: [main]

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
        with: { terraform_version: 1.9.0 }

      - name: fmt
        run: terraform fmt -check -recursive

      - name: init (no backend, fetches modules)
        run: terraform init -backend=false

      - name: validate
        run: terraform validate

      - name: tflint
        uses: terraform-linters/setup-tflint@v4
      - run: tflint --recursive

      - name: checkov
        uses: bridgecrewio/checkov-action@master
        with: { framework: terraform }

      - name: tfsec
        uses: aquasecurity/tfsec-action@v1.0.3

      - name: terraform test
        run: terraform test

  plan:
    needs: validate
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init
      - run: terraform plan -out=tfplan

  apply:
    if: github.ref == 'refs/heads/main'
    needs: plan
    runs-on: ubuntu-latest
    environment: prod   # GitHub approval gate
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - run: terraform init
      - run: terraform apply -auto-approve tfplan
```

---

## REVIEW / REFACTOR / IMPROVE / AUDIT / FIX / CLEAN-UP PROTOCOL

TRIGGER on ANY phrasing like: "review this terraform", "improve this module", "refactor", "audit", "critique", "fix", "clean up", "modernize", "what's wrong with", "can this be better". Always emit the full rewritten files.

1. Scan against rules 1–7.
2. **Rewrite the complete files** as ```hcl blocks — never a bullet list of changes, never `// rest unchanged`.
3. Brief change log goes **after** the rewritten code.

Rubric:

| # | Check | Fix |
|---|---|---|
| 1 | Any `resource "aws_*"` blocks? | Replace with `module` from terraform-aws-modules table |
| 2 | Names not in `<type>-<project>-<env>-<region>-<id>` form? | Rewrite via `locals` from required vars (including `var.identifier` suffix) |
| 3 | `?ref=` is a tag, branch, or missing? | Replace with 40-char commit SHA |
| 4 | No `create` / `create_<resource>` vars? | Add them; gate every module with `count = var.create && var.create_<r> ? 1 : 0` |
| 5 | `count` used with a list/length? | Convert to `for_each` over a map |
| 6 | Root has `resource` blocks? | Move into a child module under `modules/` |
| 7 | Files truncated/described? | Output complete files |

---

## SELF-CHECK BEFORE SENDING

| ☐ | Check |
|---|---|
| ☐ | Response contains complete ```hcl blocks for every file mentioned or implied (default: variables.tf + main.tf + outputs.tf) |
| ☐ | No prose substitutes for code ("the file would contain", "you would define", bullet lists describing fields) |
| ☐ | Zero `resource "aws_*"` — all AWS resources go through terraform-aws-modules |
| ☐ | All names follow `<type>-<project>-<env>-<region>-<id>` built from `var.project`, `var.environment`, `var.region`, `var.identifier` — the `${var.identifier}` suffix is ALWAYS present |
| ☐ | Every `source = "...?ref=..."` is a 40-char commit SHA (not a tag, branch, or registry version) |
| ☐ | Every module exposes `variable "create"` + `variable "create_<resource>"` and gates with `count = var.create && var.create_<r> ? 1 : 0` |
| ☐ | At least one `for_each = ... ` block over a map appears in every module template — `count` is reserved for the create kill-switch only |
| ☐ | Root configs contain only `module {}` blocks (plus terraform/provider/locals/data) |
| ☐ | For reviews/refactors/audits/fix-ups: emitted complete rewritten files, not change bullets |
| ☐ | For testing/CI/architecture: surfaced modules + SHA-pinning + naming + create-pattern + for_each in the example code |
