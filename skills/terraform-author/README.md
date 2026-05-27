# terraform-author

A Claude Code skill for writing, reviewing, and architecting Terraform and OpenTofu infrastructure-as-code.

## Install

```bash
npx @skills/cli install terraform-author
```

Or install all skills:

```bash
npx @skills/cli install --all
```

## Usage

Once installed, the skill activates automatically when you ask Claude Code about Terraform topics. You can also invoke it explicitly:

```
/terraform-author
```

## What it covers

- Module structure and hierarchy decisions
- Remote state configuration and locking
- Testing strategy (native Terraform tests, Terratest, Checkov)
- CI/CD pipeline setup for IaC
- Multi-environment and multi-team organization patterns
- Common pitfalls and how to avoid them

## Self-improvement

This skill is continuously improved by the autoresearch loop in this monorepo. See [`research/scenarios/terraform-author/`](../../research/scenarios/terraform-author/) for the test scenarios used to score each iteration.
